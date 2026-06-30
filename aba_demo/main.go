package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// TrueBlockSTM mô phỏng status array của blockSTM
type TrueBlockSTM struct {
	txStatus []int32
}

func main() {
	stm := &TrueBlockSTM{
		txStatus: make([]int32, 20),
	}

	// Giả lập ban đầu giao dịch index=10 vừa execute xong, trạng thái là Executed (1)
	stm.txStatus[10] = 1

	var wg sync.WaitGroup
	wg.Add(3)

	// Worker A: Lấy tx=10 ra khỏi validateCh và bắt đầu Validate
	go func() {
		defer wg.Done()
		fmt.Println("[Worker A] Lấy tx=10 từ validateCh...")
		if atomic.CompareAndSwapInt32(&stm.txStatus[10], 1, 2) {
			fmt.Println("[Worker A] CAS(1, 2) thành công! Bắt đầu validate với ReadSet CŨ...")

			// Giả lập quá trình đọc và so sánh version mất thời gian
			time.Sleep(200 * time.Millisecond)

			// Vì dùng ReadSet cũ, Worker A thấy dữ liệu vẫn khớp -> Valid
			fmt.Println("[Worker A] Đã validate xong, kết quả: HỢP LỆ! Gọi CAS(2, 3)...")
			if atomic.CompareAndSwapInt32(&stm.txStatus[10], 2, 3) {
				fmt.Println("🚨 LỖI NGHIÊM TRỌNG: [Worker A] Ghi đè trạng thái thành 3 (Validated) bằng dữ liệu CŨ!")
			} else {
				fmt.Println("✅ [Worker A] CAS(2, 3) thất bại (Hệ thống đã chặn được lỗi).")
			}
		}
	}()

	// Worker C (Cascade Validation): Một tx trước (ví dụ tx=5) vừa cập nhật data, phát hiện tx=10 cần chạy lại
	go func() {
		defer wg.Done()
		// Đợi Worker A đổi sang 2
		time.Sleep(50 * time.Millisecond)
		fmt.Println("\n[Worker C] Phát hiện tx=5 thay đổi state. Gọi Cascade Validation cho tx=10...")

		// Worker C đổi 2 về 1 để đưa lại vào queue
		if atomic.CompareAndSwapInt32(&stm.txStatus[10], 2, 1) {
			fmt.Println("[Worker C] CAS(2, 1) thành công! Đẩy tx=10 lại vào validateCh.")
		}
	}()

	// Worker B: Bắt được tx=10 từ validateCh (sau khi Worker C đẩy vào)
	go func() {
		defer wg.Done()
		// Đợi Worker C đổi về 1
		time.Sleep(100 * time.Millisecond)

		fmt.Println("\n[Worker B] Lấy tx=10 mới từ validateCh...")
		if atomic.CompareAndSwapInt32(&stm.txStatus[10], 1, 2) {
			fmt.Println("[Worker B] CAS(1, 2) thành công! Bắt đầu validate với ReadSet MỚI...")

			// Worker B validate chậm hơn Worker A một chút
			time.Sleep(200 * time.Millisecond)

			// Vì dùng ReadSet mới, Worker B phát hiện phiên bản không khớp -> Invalid -> Abort
			fmt.Println("[Worker B] Đã validate xong, kết quả: KHÔNG HỢP LỆ! Gọi CAS(2, 4) để Abort...")
			if atomic.CompareAndSwapInt32(&stm.txStatus[10], 2, 4) {
				fmt.Println("✅ [Worker B] CAS(2, 4) thành công! Tx=10 bị Abort chuẩn xác.")
			} else {
				fmt.Printf("🚨 LỖI NGHIÊM TRỌNG: [Worker B] CAS(2, 4) thất bại vì trạng thái hiện tại đã bị ai đó sửa thành %d!\n", atomic.LoadInt32(&stm.txStatus[10]))
			}
		}
	}()

	wg.Wait()

	fmt.Println("\n=============================================")
	fmt.Printf("Trạng thái cuối cùng của tx=10: %d\n", stm.txStatus[10])
	if stm.txStatus[10] == 3 {
		fmt.Println("❌ KẾT LUẬN: Đã xảy ra lỗi ABA Race Condition. Giao dịch LỖI nhưng bị đánh dấu là HỢP LỆ (3).")
	} else if stm.txStatus[10] == 4 {
		fmt.Println("✅ KẾT LUẬN: Hệ thống an toàn.")
	}
}
