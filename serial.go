package serial

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"regexp"
	"time"
)

// End of line character (AKA EOL), newline character (ASCII 10, CR, '\n'). is used by default.
const (
	EOL_DEFAULT byte = '\n'
	CMD_DELAY        = time.Millisecond * 50
)

/*******************************************************************************************
*******************************   TYPE DEFINITIONS 	****************************************
*******************************************************************************************/

type CommPort struct {
	port         io.ReadWriteCloser
	name         string
	baud         int
	eol          uint8
	rxChar       chan byte
	closeReqChan chan bool
	closeAckChan chan error
	buff         *bytes.Buffer
	logger       *log.Logger
	portIsOpen   bool
	Verbose      bool
	// openPort      func(port string, baud int) (io.ReadWriteCloser, error)
}

/*******************************************************************************************
********************************   BASIC FUNCTIONS  ****************************************
*******************************************************************************************/

func New(logToFile bool) *CommPort {
	var writer io.Writer
	if logToFile {
		// Create new file
		file, err := os.OpenFile(fmt.Sprintf("log_serial_%d.txt", time.Now().Unix()), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			log.Fatalln("Failed to open log file", ":", err)
		}
		writer = io.MultiWriter(file, os.Stdout)
	} else {
		writer = io.Writer(os.Stdout)
	}

	return &CommPort{
		logger:  log.New(writer, "PREFIX: ", log.Ldate|log.Ltime),
		eol:     EOL_DEFAULT,
		buff:    bytes.NewBuffer(make([]uint8, 256)),
		Verbose: true,
	}
}

func (sp *CommPort) Open(name string, baud int, timeout ...time.Duration) error {
	// Check if port is open
	if sp.portIsOpen {
		return fmt.Errorf("\"%s\" is already open", name)
	}
	var readTimeout time.Duration
	if len(timeout) > 0 {
		readTimeout = timeout[0]
	}
	// Open serial port
	comPort, err := openPort(name, baud, readTimeout)
	if err != nil {
		return fmt.Errorf("unable to open port \"%s\" - %s", name, err)
	}
	// Open port successful
	sp.name = name
	sp.baud = baud
	sp.port = comPort
	sp.portIsOpen = true
	sp.buff.Reset()
	// Open channels
	sp.rxChar = make(chan byte)
	// Enable threads
	go sp.readSerialPort()
	go sp.processSerialPort()
	sp.logger.SetPrefix(fmt.Sprintf("[%s] ", sp.name))
	sp.log("Serial port %s@%d open", sp.name, sp.baud)
	return nil
}

// This method close the current Serial Port.
func (sp *CommPort) Close() error {
	if sp.portIsOpen {
		sp.portIsOpen = false
		close(sp.rxChar)
		sp.log("Serial port %s closed", sp.name)
		return sp.port.Close()
	}
	return nil
}

// This method prints data trough the serial port.
func (sp *CommPort) Write(data []byte) (n int, err error) {
	if sp.portIsOpen {
		n, err = sp.port.Write(data)
		if err != nil {
			// Do nothing
		} else {
			sp.log("Tx >> %s", string(data))
		}
	} else {
		err = fmt.Errorf("serial port is not open")
	}
	return
}

// This method prints data trough the serial port.
func (sp *CommPort) Print(str string) error {
	if sp.portIsOpen {
		_, err := sp.port.Write([]byte(str))
		if err != nil {
			return err
		} else {
			sp.log("Tx >> %s", str)
		}
	} else {
		return fmt.Errorf("serial port is not open")
	}
	return nil
}

// Prints data to the serial port as human-readable ASCII text followed by a carriage return character
// (ASCII 13, CR, '\r') and a newline character (ASCII 10, LF, '\n').
func (sp *CommPort) Println(str string) error {
	return sp.Print(str + "\r\n")
}

// Printf formats according to a format specifier and print data trough the serial port.
func (sp *CommPort) Printf(format string, args ...interface{}) error {
	str := format
	if len(args) > 0 {
		str = fmt.Sprintf(format, args...)
	}
	return sp.Print(str)
}

//This method send a binary file trough the serial port. If EnableLog is active then this method will log file related data.
func (sp *CommPort) SendFile(filepath string) error {
	// Aux Vars
	sentBytes := 0
	q := 512
	var data []byte
	// Read file
	file, err := ioutil.ReadFile(filepath)
	if err != nil {
		sp.log("DBG >> %s", "Invalid filepath")
		return err
	} else {
		fileSize := len(file)
		sp.log("INF >> %s", "File size is %d bytes", fileSize)

		for sentBytes <= fileSize {
			//Try sending slices of less or equal than 512 bytes at time
			if len(file[sentBytes:]) > q {
				data = file[sentBytes:(sentBytes + q)]
			} else {
				data = file[sentBytes:]
			}
			// Write binaries
			_, err := sp.port.Write(data)
			if err != nil {
				sp.log("DBG >> %s", "Error while sending the file")
				return err
			} else {
				sentBytes += q
				time.Sleep(time.Millisecond * 100)
			}
		}
	}
	//Encode data to send
	return nil
}

// Read the first byte of the serial buffer.
func (sp *CommPort) Read() (byte, error) {
	if sp.portIsOpen {
		return sp.buff.ReadByte()
	} else {
		return 0x00, fmt.Errorf("serial port is not open")
	}
}

// Read first available line from serial port buffer.
//
// Line is delimited by the EOL character, newline character (ASCII 10, LF, '\n') is used by default.
//
// The text returned from ReadLine does not include the line end ("\r\n" or '\n').
func (sp *CommPort) ReadLine() (string, error) {
	if sp.portIsOpen {
		line, err := sp.buff.ReadString(sp.eol)
		if err != nil {
			return "", err
		} else {
			return removeEOL(line), nil
		}
	} else {
		return "", fmt.Errorf("serial port is not open")
	}
}

// Wait for a defined regular expression for a defined amount of time.
func (sp *CommPort) WaitForRegexTimeout(exp string, timeout time.Duration) (string, error) {

	if sp.portIsOpen {
		//Decode received data
		timeExpired := false

		regExpPattern := regexp.MustCompile(exp)

		//Timeout structure
		c1 := make(chan string, 1)
		go func() {
			sp.log("INF >> Waiting for RegExp: \"%s\"", exp)
			var result []string
			for !timeExpired {
				time.Sleep(CMD_DELAY)
				line, err := sp.ReadLine()
				if err != nil {
					// Do nothing
				} else {
					result = regExpPattern.FindAllString(line, -1)
					if len(result) > 0 {
						c1 <- result[0]
						break
					}
				}
			}
		}()
		select {
		case data := <-c1:
			sp.log("INF >> The RegExp: \"%s\"", exp)
			sp.log("INF >> Has been matched: \"%s\"", data)
			return data, nil
		case <-time.After(timeout):
			timeExpired = true
			sp.log("INF >> Unable to match RegExp: \"%s\"", exp)
			return "", fmt.Errorf("timeout expired")
		}
	} else {
		return "", fmt.Errorf("serial port is not open")
	}
}

// Available return the total number of available unread bytes on the serial buffer.
func (sp *CommPort) Available() int {
	return sp.buff.Len()
}

// Change end of line character (AKA EOL), newline character (ASCII 10, LF, '\n') is used by default.
func (sp *CommPort) EOL(c byte) {
	sp.eol = c
}

/*******************************************************************************************
******************************   PRIVATE FUNCTIONS  ****************************************
*******************************************************************************************/

func (sp *CommPort) readSerialPort() {
	rxBuff := make([]byte, 256)
	for sp.portIsOpen {
		n, _ := sp.port.Read(rxBuff)
		// Write data to serial buffer
		sp.buff.Write(rxBuff[:n])
		for _, b := range rxBuff[:n] {
			if sp.portIsOpen {
				sp.rxChar <- b
			}
		}
	}
}

func (sp *CommPort) processSerialPort() {
	screenBuff := make([]byte, 0)
	var lastRxByte byte
	for {
		if sp.portIsOpen {
			lastRxByte = <-sp.rxChar
			// Print received lines
			switch lastRxByte {
			case sp.eol:
				// EOL - Print received data
				sp.log("Rx << %s", string(append(screenBuff, lastRxByte)))
				screenBuff = make([]byte, 0) //Clean buffer
				break
			default:
				screenBuff = append(screenBuff, lastRxByte)
			}
		} else {
			break
		}
	}
}

func (sp *CommPort) log(format string, a ...interface{}) {
	if sp.Verbose {
		sp.logger.Printf(format, a...)
	}
}

func removeEOL(line string) string {
	var data []byte
	// Remove CR byte "\r"
	for _, b := range []byte(line) {
		switch b {
		case '\r':
			// Do nothing
		case '\n':
			// Do nothing
		default:
			data = append(data, b)
		}
	}
	return string(data)
}

// Converts the timeout values for Linux / POSIX systems
func posixTimeoutValues(readTimeout time.Duration) (vmin uint8, vtime uint8) {
	const MAXUINT8 = 1<<8 - 1 // 255
	// set blocking / non-blocking read
	var minBytesToRead uint8 = 1
	var readTimeoutInDeci int64
	if readTimeout > 0 {
		// EOF on zero read
		minBytesToRead = 0
		// convert timeout to deciseconds as expected by VTIME
		readTimeoutInDeci = readTimeout.Nanoseconds() / 1e6 / 100
		// capping the timeout
		if readTimeoutInDeci < 1 {
			// min possible timeout 1 deciseconds (0.1s)
			readTimeoutInDeci = 1
		} else if readTimeoutInDeci > MAXUINT8 {
			// max possible timeout is 255 deciseconds (25.5s)
			readTimeoutInDeci = MAXUINT8
		}
	}
	return minBytesToRead, uint8(readTimeoutInDeci)
}
