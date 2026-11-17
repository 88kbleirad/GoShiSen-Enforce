# Đóng góp cho GoShiSen-Enforce

## Trước khi bắt đầu

- Go ≥ 1.22 và `make`.
- Đọc `docs/design/architecture.md` và `docs/design/mvp-scope.md`: đây là các tài liệu chuẩn. Mọi thay đổi về hành vi phải cập nhật chúng trong cùng một pull request.

## Quy trình làm việc

```bash
git clone https://github.com/88kbleirad/GoShiSen-Enforce
cd GoShiSen-Enforce
make check          # fmt + vet + test + build — bắt buộc phải đạt trước khi bạn push
```

CI (`.github/workflows/ci.yml`) chạy cùng cổng kiểm tra đó cộng thêm `go test -race ./...`.

## Các quy tắc

1. **Không có mã giả.** Không thêm các package rỗng, stub, hoặc TODO được trình bày như các tính năng. Một package chỉ được đưa vào khi có mã hoạt động và test kèm theo, hoặc không đưa vào gì cả.
2. **Không có thay đổi hệ thống không liên quan.** Daemon không được đụng tới nftables, routes, hoặc sysctls, trừ trong giai đoạn sở hữu hành vi đó.
3. **Các dependency mới cần có lý do hợp lý** trong mô tả PR (tại sao thư viện chuẩn hoặc một dependency hiện có là không đủ).
4. **Giữ tài liệu đồng bộ.** Trạng thái trong README, `docs/user-guide.md`, và `docs/developer-guide.md` phải mô tả đúng kho lưu trữ hiện tại.
5. **Lỗi phải có thể hành động**: nêu tên trường, giá trị vi phạm, và cách khắc phục.

## Commit và pull requests

- Commit nhỏ, tập trung; mỗi commit xử lý một vấn đề.
- Mô tả PR: thay đổi gì, tại sao, và chính xác lệnh bạn đã chạy để xác minh.
- Tham chiếu giai đoạn/tiêu chí chấp nhận từ `mvp-scope.md` mà thay đổi đáp ứng.
