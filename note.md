Lệnh "phép thuật" mà tôi vừa dùng trên máy 234 của bạn cực kỳ đơn giản:

bash
sudo ip route del default proto static
Giải thích lệnh: ip route del default nghĩa là xóa bỏ tuyến đường mặc định ra Internet. Chữ proto static nghĩa là chỉ xóa cái tuyến đường tĩnh của cái IP .234. Khi đường tĩnh bị xóa, Linux sẽ tự động dùng cái đường dự phòng còn lại (chính là tuyến đường động DHCP của IP .55) để đi ra ngoài!

⚠️ Nhưng có một lưu ý rất quan trọng: Lệnh tôi vừa gõ nó chỉ thay đổi trong bộ nhớ RAM tạm thời. Nếu bạn khởi động lại máy (Reboot), hệ điều hành sẽ nạp lại file cấu hình gốc và lỗi sẽ bị lặp lại!

Để "phép thuật" này có tác dụng vĩnh viễn, bạn cần sửa cấu hình gốc. Lát nữa nếu rảnh, bạn hãy mở file /etc/netplan/01-netcfg.yaml trên máy 234 ra và Xóa bỏ 3 dòng này đi:

yaml
routes:
        - to: 0.0.0.0/0
          via: 192.168.1.1
Ý nghĩa của việc xóa 3 dòng trên là: "Khai báo IP tĩnh .234 chỉ để xài mạng LAN nội bộ, cấm tiệt không cho làm Gateway chỉ đường ra Internet". Xóa xong bạn gõ lệnh sudo netplan apply là lưu lại vĩnh viễn. Đảm bảo từ nay về sau khởi động lại máy cỡ nào cũng mượt mà như nhung!