package main

import (
	"flag"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/applepi-icpc/serial"
)

var (
	flagSerialDevice = flag.String("serial", "/dev/tty.SLAB_USBtoUART", "Serial TTY")
)

var (
	initOnce sync.Once
	port     *serial.SerialPort
)

func InitSerialDevice() {
	initOnce.Do(func() {
		port = serial.New()

		err := port.Open(*flagSerialDevice, 9600, 3*time.Second)
		if err != nil {
			log.Fatal(err)
		}
	})
}

func SerialReadByte(timeout time.Duration) (byte, error) {
	InitSerialDevice()

	ch := make(chan byte, 1)
	timeExpired := false
	go func() {
		for !timeExpired {
			b, err := port.Read()
			if err != nil {
				time.Sleep(100 * time.Millisecond)
			} else {
				ch <- b
				break
			}
		}
	}()

	var res byte
	select {
	case res = <-ch:
		return res, nil
	case <-time.After(timeout):
		timeExpired = true
		return 0x00, fmt.Errorf("timeout")
	}
}

var begin = []byte{0x32, 0x3D, 0x00, 0x1C}

func SerialWaitSignal() error {
	var recognized = 0
	for {
		if recognized == len(begin) {
			return nil
		}
		b, err := SerialReadByte(2000 * time.Millisecond)
		if err != nil {
			return err
		}
		if b == begin[recognized] {
			recognized++
		} else {
			recognized = 0
		}
	}
}

const FrameLength = 28

func SerialReadFrame() ([]byte, error) {
	data := make([]byte, FrameLength)
	sum := 0
	for _, v := range begin {
		sum += int(v)
	}
	for i := 0; i < FrameLength; i++ {
		var err error
		data[i], err = SerialReadByte(2000 * time.Millisecond)
		if err != nil {
			return nil, err
		}
		if i < 26 {
			sum += int(data[i])
		}
	}
	checksum := (int(data[26]) << 8) + int(data[27])
	if checksum != sum {
		return nil, fmt.Errorf("error: bad checksum: %X <=> %X", checksum, sum)
	}
	return data, nil
}
