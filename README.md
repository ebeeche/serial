# Serial

Golang package for serial port

[![GoDoc](http://godoc.org/github.com/argandas/serial?status.svg)](http://godoc.org/github.com/argandas/serial)

A Go package that allow you to read and write from the serial port.
Forked from [github.com/argandas/serial](https://github.com/argandas/serial)

## So, what's different from the original repository?
* *WaitForRegexTimeout:* Added a delay before reading each line from  serial port (SimCom SIM800L compatibility)
* Renamed SerialPort to CommPort to fix a name conflict (according to GoLand IDE)
* Fixed redundant variable declarations
* Removed unreachable code
* Changed case of error messages to comply with Go's case convention
* Spelling errors
* Added option to log only to Stdout or both file and Stdout
* Removed useless *doc.go* file


## Usage

```go
package main

import (
	"github.com/ebeeche/serial"
	"time"
)

func main() {
    // log to file and stdout
	sp := serial.New(true)

	err := sp.Open("/dev/cu.usbserial", 115200)
	if err != nil {
		panic(err)
	}

	// make sure we close the port just before the program ends
	defer func() {
		err := sp.Close()
		if err != nil {
			println("error closing serial port")
		}
	}()

	// send AT command
	err = sp.Println("AT")
	if err != nil {
		panic(err)
	}

	// wait for "OK" from port
	_, err = sp.WaitForRegexTimeout("OK.*", time.Second * 3)
	if err != nil {
		panic(err)
	}
}
```

## NonBlocking Mode

By default the returned serial port reads in blocking mode. Which means `Read()` will block until at least one byte is returned. If that's not what you want, specify a positive ReadTimeout and the Read() will timeout returning 0 bytes if no bytes are read.  Please note that this is the total timeout the read operation will wait and not the interval timeout between two bytes.

```
    sp := serial.New()
    err := sp.Open("/dev/cu.usbserial", 115200, time.Second * 5)
```