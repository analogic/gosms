package modem

import (
	"github.com/tarm/serial"
	"log"
	"strings"
	"errors"
	"fmt"
	"bytes"
	"regexp"
	"strconv"
	"unicode/utf16"
	"math/rand"
	"time"
)

type Driver struct {
	ComPort  string
	BaudRate int
	Port     *serial.Port
	DeviceId string
}

func New(ComPort string, BaudRate int, DeviceId string) (modem *Driver) {
	modem = &Driver{ComPort: ComPort, BaudRate: BaudRate, DeviceId: DeviceId}
	return modem
}

func (m *Driver) Connect() (err error) {
	config := &serial.Config{Name: m.ComPort, Baud: m.BaudRate, ReadTimeout: time.Second * 5} // read timeout should not happen if modem will behave nicely
	m.Port, err = serial.OpenPort(config)

	if err == nil {
		m.initModem()
	}

	return err
}

func (m *Driver) initModem() {
	m.SendCommand("ATE0\r\n", true) // echo off
	m.SendCommand("AT+CMEE=1\r\n", true) // useful error messages
	m.SendCommand("AT+WIND=0\r\n", true) // disable notifications
	m.SendCommand("AT+CMGF=1\r\n", true) // switch to Text SMS Mode mode
	m.SendCommand("AT+CSCS=\"UCS2\"\r\n", true); // switch to ucs2 communication
	m.SendCommand("AT+CPMS=\"MT\"\r\n", true) // read SMS messages from SIM and device memory
}

func (m *Driver) Expect(possibilities []string) (string, error) {
	buffer := make([]byte, 128)
	var output bytes.Buffer

	for {
		c, err := m.Port.Read(buffer)
		output.WriteString(string(buffer[:c]))

		for _, possibility := range possibilities {
			if strings.Contains(output.String(), possibility) {
				m.log("--- Expect:", strings.Join(possibilities, "|"), "Got:", output.String());
				return output.String(), nil
			}
		}

		if err != nil {
			break;
		}
	}

	m.log("--- Expect:", strings.Join(possibilities, "|"), "Got:", output.String(), "(match not found!)");
	return output.String(), errors.New("match not found")
}

func (m *Driver) Send(command string) {
	m.log("--- Send:", command)
	_, err := m.Port.Write([]byte(command))
	if err != nil {
		log.Fatal(err)
	}
}

func (m *Driver) SendCommand(command string, waitForOk bool) (output string) {
	m.Send(command)

	if waitForOk {
		output, _ = m.Expect([]string{"OK\r\n", "ERROR\r\n"}) // we will not change api so errors are ignored for now
		time.Sleep(time.Millisecond * 100)
	}

	return output
}

func (m *Driver) SendSMS(mobile string, message string) (sent bool, err error) {
	log.Println("--- SendSMS ", mobile, message)

	if IsASCII(message) {
		m.SendCommand("AT+CSMP=17,167,0,0\r\n", true);
	} else {
		m.SendCommand("AT+CSMP=17,167,0,8\r\n", true);
	}

	if IsASCII(message) && len(message) > 160 {
		return m.sendConcatenatedSMS(mobile, message)
	} else if IsASCII(message) != true && len(message) > 70 {
		return m.sendConcatenatedSMS(mobile, message)
	} else {
		return m.sendSingleSMS(mobile, message)
	}
}

func (m *Driver) sendSingleSMS(mobile string, message string) (sent bool, err error) {
	mobile = ASCII2UCS2HEX(mobile)
	message = ASCII2UCS2HEX(message)

	m.Send("AT+CMGS=\""+mobile+"\"\r") // should return ">"

	m.Expect([]string{">"})
	time.Sleep(time.Millisecond * 100)

	// EOM CTRL-Z = 26
	m.Send(message+string(26));

	output, err := m.Expect([]string{"OK\r\n", "ERROR\r\n"})
	time.Sleep(time.Millisecond * 100)

	if err != nil {
		log.Println("Invalid response to send SMS:", output)
		return false, nil // we will try again
	}

	if strings.HasSuffix(output, "OK\r\n") {
		return true, nil
	} else { // ERROR
		return false, errors.New("ERROR")
	}
}

func (m *Driver) sendConcatenatedSMS(mobile string, message string) (sent bool, err error) {
	var messages *[]string
	if IsASCII(message) {
		messages = splitString(message, 153) // UDH len in septets = 7
	} else {
		messages = splitString(message, 67)
	}

	ref := rand.Int() % 255
	total := len(*messages);

	var status string
	for i, message := range *messages {
		// messagem, (153 char 7-bit / 67 char ucs2)

		m.Send(fmt.Sprintf("AT^SCMS=%s,145,%d,%d,%d,%d\r", ASCII2UCS2HEX(mobile), i + 1, total, 8, ref))

		m.Expect([]string{">"})
		time.Sleep(time.Millisecond * 100)

		m.Send(ASCII2UCS2HEX(message)+string(26));

		status, err = m.Expect([]string{"OK\r\n", "ERROR\r\n"})
		time.Sleep(time.Millisecond * 100)

		if err != nil {
			log.Println("Invalid response to send SMS:", status)
			break
		}
	}

	if strings.HasSuffix(status, "OK\r\n") {
		return true, nil
	} else { // ERROR
		return false, errors.New("ERROR")
	}
}

func (m *Driver) ReadSMS() (*[][]string) {

	/*
	1. index
	2. status
	3. originator
	4. name
	5. timestamp
	6. message
	 */
	r := regexp.MustCompile(`\+CMGL: (\d+),"(ALL|REC READ|REC UNREAD)","([0-9a-fA-F]+)",([^,]*),"([^"]+)"\r?\n([0-9a-fA-F]*)\r?\n`);

	output := m.SendCommand("AT+CMGL=\"ALL\"\r\n", true);
	matches := r.FindAllStringSubmatch(output, -1);
	var messages [][]string

	for _, match := range matches {

		log.Println("---> incoming message", match[1])
		log.Printf("     status: %v, originator: %s, name: %s, timestamp: %s\n", match[2], UCS2HEX2ASCII(match[3]), UCS2HEX2ASCII(match[4]), match[5])
		log.Println("    ", UCS2HEX2ASCII(match[6]), "\n")

		messages = append(messages, []string{UCS2HEX2ASCII(match[3]), UCS2HEX2ASCII(match[6])})

		index, _ := strconv.Atoi(match[1]);
		m.DeleteSMS(index)
	}

	return &messages
}

func (m *Driver) DeleteSMS(index int) (string, error) {
	return m.SendCommand(fmt.Sprintf("AT+CMGD=%d\r\n", index), true), nil
}

func (m *Driver) log(messages... interface{}) {
	for key, message := range messages {
		switch message.(type) {
			case string:
				clean := strings.Replace(message.(string), "\r\n", "\\r\\n", -1);
				messages[key] = strings.Replace(clean, "\r", "\\r", -1);
		}
	}

	log.Println(messages);
}

func ASCII2UCS2HEX(input string) string {
	hex := fmt.Sprintf("%04x", utf16.Encode([]rune(input)))
	return strings.Replace(hex[1:len(hex)-1], " ", "", -1)
}

func UCS2HEX2ASCII(input string) string {
	output := ""
	for i := 0; i*4 < len(input); i += 1 {
		n, err := strconv.ParseInt(input[(i*4):(i*4+4)], 16, 32)
		if err != nil {
			log.Fatal(err)
		}
		output += string(n)
	}

	return output
}

func IsASCII(s string) bool {
	for _, c := range s {
		if c > 127 {
			return false
		}
	}
	return true
}

func splitString(s string, n int) *[]string {
	sub := ""
	subs := []string{}

	runes := bytes.Runes([]byte(s))
	l := len(runes)
	for i, r := range runes {
		sub = sub + string(r)
		if (i + 1) % n == 0 {
			subs = append(subs, sub)
			sub = ""
		} else if (i + 1) == l {
			subs = append(subs, sub)
		}
	}

	return &subs
}