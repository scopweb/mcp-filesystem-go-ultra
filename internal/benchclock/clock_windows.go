package benchclock

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// QPC avoids the coarser time.Now clock observed on Windows. The pointer is
// used only for the synchronous Windows API output parameter (no retention).
var kernel = windows.NewLazySystemDLL("kernel32.dll")
var counter = kernel.NewProc("QueryPerformanceCounter")
var frequency = func() int64 {
	var f int64
	ok, _, err := kernel.NewProc("QueryPerformanceFrequency").Call(uintptr(unsafe.Pointer(&f)))
	if ok == 0 || f <= 0 {
		panic(err)
	}
	return f
}()

func Now() int64 {
	var ticks int64
	ok, _, err := counter.Call(uintptr(unsafe.Pointer(&ticks)))
	if ok == 0 {
		panic(err)
	}
	return ticks
}

func Since(start int64) time.Duration {
	ticks := Now() - start
	return time.Duration(ticks/frequency)*time.Second + time.Duration((ticks%frequency)*int64(time.Second)/frequency)
}
