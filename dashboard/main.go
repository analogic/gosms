package main

import (
	"fmt"
	"github.com/haxpax/gosms"
	"github.com/haxpax/gosms/modem"
	"log"
	"os"
	"strconv"
)

func main() {

	log.Println("main: ", "Initializing gosms")
	//load the config, abort if required config is not preset
	appConfig, err := gosms.GetConfig("conf.ini")
	if err != nil {
		log.Println("main: ", "Invalid config: ", err.Error(), " Aborting")
		os.Exit(1)
	}

	db, err := gosms.InitDB("sqlite3", "db.sqlite")
	if err != nil {
		log.Println("main: ", "Error initializing database: ", err, " Aborting")
		os.Exit(1)
	}
	defer db.Close()

	serverhost, _ := appConfig.Get("SETTINGS", "SERVERHOST")
	serverport, _ := appConfig.Get("SETTINGS", "SERVERPORT")

	serverusername, _ := appConfig.Get("SETTINGS", "USERNAME")
	serverpassword, _ := appConfig.Get("SETTINGS", "PASSWORD")

	smtpenabledraw, _ := appConfig.Get("SETTINGS", "SMTPENABLED")
	smtphost, _ := appConfig.Get("SETTINGS", "SMTPHOST")
	smtpportraw, _ := appConfig.Get("SETTINGS", "SMTPPORT")
	smtpauthraw, _ := appConfig.Get("SETTINGS", "SMTPAUTH")
	smtpusername, _ := appConfig.Get("SETTINGS", "SMTPUSERNAME")
	smtppassword, _ := appConfig.Get("SETTINGS", "SMTPPASSWORD")
	smtpsender, _ := appConfig.Get("SETTINGS", "SMTPSENDER")
	smtprecipient, _ := appConfig.Get("SETTINGS", "SMTPRECIPIENT")

	smtpenabled, _ := strconv.Atoi(smtpenabledraw)
	smtpport, _ := strconv.Atoi(smtpportraw)
	smtpauth, _ := strconv.Atoi(smtpauthraw)

	smtp := gosms.SMTP{
		Enabled: smtpenabled == 1,
		Host: smtphost,
		Port: smtpport,
		Auth: smtpauth == 1,
		Username: smtpusername,
		Password: smtppassword,
		Sender: smtpsender,
		Recipient: smtprecipient,
	}

	_numDevices, _ := appConfig.Get("SETTINGS", "DEVICES")
	numDevices, _ := strconv.Atoi(_numDevices)
	log.Println("main: number of devices: ", numDevices)

	var modems []*modem.Driver
	for i := 0; i < numDevices; i++ {
		dev := fmt.Sprintf("DEVICE%v", i)
		_port, _ := appConfig.Get(dev, "COMPORT")
		_baud := 115200 //appConfig.Get(dev, "BAUDRATE")
		_devid, _ := appConfig.Get(dev, "DEVID")
		m := modem.New(_port, _baud, _devid)
		modems = append(modems, m)
	}

	_bufferSize, _ := appConfig.Get("SETTINGS", "BUFFERSIZE")
	bufferSize, _ := strconv.Atoi(_bufferSize)

	_bufferLow, _ := appConfig.Get("SETTINGS", "BUFFERLOW")
	bufferLow, _ := strconv.Atoi(_bufferLow)

	_loaderTimeout, _ := appConfig.Get("SETTINGS", "MSGTIMEOUT")
	loaderTimeout, _ := strconv.Atoi(_loaderTimeout)

	_loaderCountout, _ := appConfig.Get("SETTINGS", "MSGCOUNTOUT")
	loaderCountout, _ := strconv.Atoi(_loaderCountout)

	_loaderTimeoutLong, _ := appConfig.Get("SETTINGS", "MSGTIMEOUTLONG")
	loaderTimeoutLong, _ := strconv.Atoi(_loaderTimeoutLong)

	log.Println("main: Initializing worker")
	gosms.InitWorker(modems, bufferSize, bufferLow, loaderTimeout, loaderCountout, loaderTimeoutLong, &smtp)

	log.Println("main: Initializing server")
	err = InitServer(serverhost, serverport, serverusername, serverpassword)
	if err != nil {
		log.Println("main: ", "Error starting server: ", err.Error(), " Aborting")
		os.Exit(1)
	}
}
