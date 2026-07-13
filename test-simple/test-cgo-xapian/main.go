package main

/*
#cgo CXXFLAGS: -std=c++17
#cgo LDFLAGS: -lxapian -lstdc++ -lpthread
#include "wrapper.h"
*/
import "C"
import (
	"fmt"
	"sync"
	"time"
	"runtime"
)

func main() {
	dbPath := C.CString("./cgo_test_db")
	
	fmt.Println("Khởi tạo DB mẫu...")
	C.CreateSampleDb(dbPath)
	
	numGoroutines := 1000
	iterations := 100
	
	// Khởi chạy 1 luồng Write chạy ngầm
	go func() {
		for k := 0; k < 2000; k++ {
			C.WriteDb(dbPath, C.int(k))
			time.Sleep(1 * time.Millisecond)
		}
	}()
	
	var wg sync.WaitGroup
	wg.Add(numGoroutines)
	
	start := time.Now()
	
	fmt.Printf("Bắt đầu spam %d goroutines, mỗi goroutine gọi CGO search %d lần...\n", numGoroutines, iterations)
	
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			defer wg.Done()
			success := 0
			for j := 0; j < iterations; j++ {
				res := C.SearchDb(dbPath)
				if int(res) > 0 {
					success++
				}
			}
			fmt.Printf("Goroutine %d hoàn thành: %d thành công\n", id, success)
		}(i)
	}
	
	wg.Wait()
	fmt.Printf("Hoàn tất tất cả trong %v\n", time.Since(start))
}
