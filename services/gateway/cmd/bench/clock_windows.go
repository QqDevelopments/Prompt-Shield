package main

import (
	"errors"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// QPC measures short intervals without the Windows interrupt-clock granularity.
func newMeasurementClock() (measurementClock, error) {
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	counter := kernel.NewProc("QueryPerformanceCounter")
	frequencyProc := kernel.NewProc("QueryPerformanceFrequency")
	if err := counter.Find(); err != nil {
		return measurementClock{}, err
	}
	if err := frequencyProc.Find(); err != nil {
		return measurementClock{}, err
	}
	var frequency int64
	ok, _, _ := frequencyProc.Call(uintptr(unsafe.Pointer(&frequency)))
	if ok == 0 || frequency <= 0 {
		return measurementClock{}, errors.New("performance counter unavailable")
	}
	read := func() int64 {
		var ticks int64
		ok, _, _ := counter.Call(uintptr(unsafe.Pointer(&ticks)))
		if ok == 0 {
			return -1
		}
		return ticks
	}
	elapsed := func(start int64) time.Duration {
		now := read()
		if start < 0 || now < start {
			return -1
		}
		ticks := now - start
		return time.Duration(ticks/frequency)*time.Second + time.Duration(float64(ticks%frequency)*float64(time.Second)/float64(frequency))
	}
	return measurementClock{now: read, since: elapsed, name: "QueryPerformanceCounter", frequency: frequency}, nil
}
