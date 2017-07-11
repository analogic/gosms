package modem

import (
	"github.com/tarm/serial"
	"log"
	"strings"
	"time"
	"errors"
	"fmt"
	"bytes"
	"regexp"
	"strconv"
	"unicode/utf16"
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
	config := &serial.Config{Name: m.ComPort, Baud: m.BaudRate, ReadTimeout: 5*time.Second} // read timeout should not happen if modem will behave nicely
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
	m.SendCommand("AT+CMGF=1\r\n", true) // switch to TEXT mode
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
	m.Port.Flush()
	_, err := m.Port.Write([]byte(command))
	if err != nil {
		log.Fatal(err)
	}
}

func (m *Driver) Read(n int) string {
	var output string = "";
	buf := make([]byte, n)
	for i := 0; i < n; i++ {
		// ignoring error as EOF raises error on Linux
		c, _ := m.Port.Read(buf)
		if c > 0 {
			output = string(buf[:c])
		}
	}

	m.log("--- Read(", n, "): ", output)
	return output
}

func (m *Driver) SendCommand(command string, waitForOk bool) string {
	m.Send(command)

	if waitForOk {
		output, _ := m.Expect([]string{"OK\r\n", "ERROR\r\n"}) // we will not change api so errors are ignored for now
		return output
	} else {
		return m.Read(1)
	}
}

func (m *Driver) SendSMS(mobile string, message string) (sent bool, err error) {
	log.Println("--- SendSMS ", mobile, message)

	if IsASCII(message) {
		m.SendCommand("AT+CSMP=17,167,0,0\r\n", true);
	} else {
		m.SendCommand("AT+CSMP=17,167,0,8\r\n", true);
	}

	mobile = ASCII2UCS2HEX(mobile)
	message = ASCII2UCS2HEX(message)

	m.Send("AT+CMGS=\""+mobile+"\"\r") // should return ">"
	m.Read(3)

	// EOM CTRL-Z = 26
	m.Send(message+string(26));
	output, err := m.Expect([]string{"OK\r\n", "ERROR\r\n"})

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