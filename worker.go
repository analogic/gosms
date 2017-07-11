package gosms

import (
	"log"
	"time"
	"math/rand"
	"github.com/haxpax/gosms/modem"
	"net/smtp"
	"fmt"
	"encoding/base64"
)

//TODO: should be configurable
const SMSRetryLimit = 3

const (
	SMSPending   = iota // 0
	SMSProcessed        // 1
	SMSError            // 2
)

type OutgoingSMS struct {
	Id 	  int    `json:"id"`
	UUID      string `json:"uuid"`
	Mobile    string `json:"mobile"`
	Body      string `json:"body"`
	Status    int    `json:"status"`
	Retries   int    `json:"retries"`
	Device    string `json:"device"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type IncomingSMS struct {
	Id 	  int    `json:"id"`
	Mobile    string `json:"mobile"`
	Body      string `json:"body"`
	Device    string `json:"device"`
	CreatedAt string `json:"created_at"`
}


type Device struct {
	Driver *modem.Driver
	Send   chan OutgoingSMS
	Poll   chan bool
}

type SMTP struct {
	Enabled    bool
	Host 	   string
	Port       int
	Auth       bool
	Username   string
	Password   string
	Sender     string
	Recipient  string
}

var devices []*Device

var queue chan OutgoingSMS
var send chan OutgoingSMS
var poll chan bool

var wakeupMessageLoader chan bool

var bufferMaxSize int
var bufferLowCount int
var messageCountSinceLastWakeup int
var timeOfLastWakeup time.Time
var messageLoaderTimeout time.Duration
var messageLoaderCountout int
var messageLoaderLongTimeout time.Duration
var smtpSettings *SMTP


func InitWorker(drivers []*modem.Driver, bufferSize, bufferLow, loaderTimeout, countOut, loaderLongTimeout int, smtp *SMTP) {
	log.Println("--- InitWorker")

	bufferMaxSize = bufferSize
	bufferLowCount = bufferLow
	messageLoaderTimeout = time.Duration(loaderTimeout) * time.Minute
	messageLoaderCountout = countOut
	messageLoaderLongTimeout = time.Duration(loaderLongTimeout) * time.Minute

	wakeupMessageLoader = make(chan bool, 1)
	wakeupMessageLoader <- true
	messageCountSinceLastWakeup = 0
	timeOfLastWakeup = time.Now().Add((time.Duration(loaderTimeout) * -1) * time.Minute) //older time handles the cold start state of the system
	smtpSettings = smtp

	// init global channels
	queue = make(chan OutgoingSMS, bufferMaxSize)
	send = make(chan OutgoingSMS, bufferMaxSize)
	poll = make(chan bool, 1)

	// init all devices
	for _, driver := range drivers {
		err := driver.Connect()
		if err != nil {
			log.Fatalln("InitWorker: error connecting", driver.DeviceId, err)
		}

		device := Device{
			Driver: driver,
			Send: make(chan OutgoingSMS, bufferMaxSize),
			Poll: make(chan bool, 1),
		};
		devices = append(devices, &device)

		go device.Worker()
	}

	// init sms/poll listener
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		go func() {
			for t := range ticker.C {
				log.Println("Polling time", t)
				poll <- true
			}
		}()

		for {
			select {
			case message := <- send:
				// we select random sending device
				rand.Seed(time.Now().Unix())
				n := rand.Int() % len(devices)
				devices[n].Send <- message
			case message := <- queue:
				// select should work at random, so if queue will be full and we will have new request
				// for send, it should pass through nearly realtime
				rand.Seed(time.Now().Unix())
				n := rand.Int() % len(devices)
				devices[n].Send <- message
			case <- poll:
				// poll all devices
				for _, device := range devices {
					device.Poll <- true
				}
			}
		}
	}()

	// load older messages
	go messageLoader(bufferSize, bufferLowCount)
}


func SendMessage(message *OutgoingSMS) {
	log.Println("--- SendMessage", message)
	err := insertOutgoingMessage(message);
	if err != nil {
		log.Fatalln("DB error: ", err)
	}

	// we try to send message immediately
	send <- *message
}

func EnqueueMessage(message *OutgoingSMS) {
	log.Println("--- EnqueueMessage: ", message)

	//notify the message loader only if its been to too long
	//or too many messages since last notification
	messageCountSinceLastWakeup++
	if messageCountSinceLastWakeup > messageLoaderCountout || time.Now().Sub(timeOfLastWakeup) > messageLoaderTimeout {
		log.Println("EnqueueMessage: ", "waking up message loader")
		wakeupMessageLoader <- true
		messageCountSinceLastWakeup = 0
		timeOfLastWakeup = time.Now()
	}
	log.Println("EnqueueMessage - anon: count since last wakeup: ", messageCountSinceLastWakeup)
}

func messageLoader(bufferSize, minFill int) {
	// Load pending messages from database as needed
	for {

		/*
		   - set a fairly long timeout for wakeup
		   - if there are very few number of messages in the system and they failed at first go,
		   and there are no events happening to call EnqueueMessage, those messages might get
		   stalled in the system until someone knocks on the API door
		   - we can afford a really long polling in this case
		*/
		timeout := make(chan bool, 1)
		go func() {
			time.Sleep(messageLoaderLongTimeout)
			timeout <- true
		}()
		log.Println("messageLoader: ", "waiting for wakeup call")
		select {
		case <-wakeupMessageLoader:
			log.Println("messageLoader: woken up by channel call")
		case <-timeout:
			log.Println("messageLoader: woken up by timeout")
		}
		if len(queue) >= bufferLowCount {
			//if we have sufficient number of messages to process,
			//don't bother hitting the database
			log.Println("messageLoader: ", "I have sufficient messages")
			continue
		}

		countToFetch := bufferMaxSize - len(queue)
		log.Println("messageLoader: ", "I need to fetch more messages", countToFetch)
		pendingMsgs, err := getPendingOutgoingMessages(countToFetch)
		if err == nil {
			log.Println("messageLoader: ", len(pendingMsgs), " pending messages found")
			for _, msg := range pendingMsgs {
				queue <- msg
			}
		}
	}
}

func (d *Device) Worker() {
	for {
		select {
		case message := <- d.Send:
			d.processMessage(message)
		case <- d.Poll:
			d.pollMessages()
		}
	}
}

func (d *Device) processMessage(message OutgoingSMS) {
	log.Println("processing: ", message.UUID, d.Driver.DeviceId)
	sent, err := d.Driver.SendSMS(message.Mobile, message.Body)

	if sent == true {
		message.Status = SMSProcessed
	} else if err == nil {
		message.Status = SMSPending
	} else {
		message.Status = SMSError
	}

	message.Device = d.Driver.DeviceId
	message.Retries++

	if err := updateOutgoingMessageStatus(message); err != nil {
		log.Fatalln("DB error: ", err)
	}

	if message.Status != SMSProcessed && message.Retries < SMSRetryLimit {
		// push message back to queue until either it is sent successfully or
		// retry count is reached
		// I can't push it to channel directly. Doing so may cause the sms to be in
		// the queue twice. I don't want that
		EnqueueMessage(&message)
	}
}

func (d *Device) pollMessages() {
	log.Println("polling: ", d.Driver.DeviceId)
	for _, message := range *d.Driver.ReadSMS() {
		sms := IncomingSMS{
			Device: d.Driver.DeviceId,
			Mobile: message[0],
			Body: message[1],
		}

		if err := insertIncomingMessage(&sms); err != nil {
			log.Fatalln("DB error: ", err)
		}

		go incomingNotice(sms)
	}
}

func incomingNotice(sms IncomingSMS) {
	if smtpSettings.Enabled == false {
		return
	}

	var auth smtp.Auth
	if smtpSettings.Auth {
		auth = smtp.PlainAuth(
			smtpSettings.Sender,
			smtpSettings.Username,
			smtpSettings.Password,
			smtpSettings.Host)
	}

	log.Println("Sending mail to", smtpSettings.Recipient,"via",fmt.Sprintf("%v:%d", smtpSettings.Host, smtpSettings.Port))
	err := smtp.SendMail(
		fmt.Sprintf("%v:%d", smtpSettings.Host, smtpSettings.Port),
		auth,
		smtpSettings.Sender,
		[]string{smtpSettings.Recipient},
		[]byte(fmt.Sprintf(
			"From: %s\r\n" +
			"To: %s\r\n" +
			"Content-Type: text/plain; charset=\"utf-8\"\r\n" +
			"Content-Transfer-Encoding: base64\r\n" +
			"Subject: SMS message from %s\r\n" +
			"\r\n" +
			"%s",
			smtpSettings.Sender,
			smtpSettings.Recipient,
			sms.Mobile,
			base64.StdEncoding.EncodeToString([]byte(sms.Body)),
		)),
	);
	if err != nil {
		log.Println("SMTP error: ", err)
	}
}