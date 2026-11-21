# GoShiSen-Enforce

**Tường lửa phòng lab Linux: engine chính sách Go trên NFQUEUE, conntrack hạt nhân, hook nftables.**

> **Trạng thái:** bản phát hành mới nhất và các giới hạn đã biết trên [Releases](https://github.com/88kbleirad/GoShiSen-Enforce/releases) và trong `docs/release/`.

## Tính năng

- **Fail-closed theo thiết kế**: backend NFQUEUE không bao giờ đặt `NFQA_CFG_F_FAIL_OPEN`, nên tràn hàng đợi hoặc thiếu listener không bao giờ vô hiệu hoá tường lửa.
- **Engine chính sách + lọc có trạng thái** trên NFQUEUE và conntrack hạt nhân. Công bố chính sách nguyên tử, bền vững (SIGHUP và REST API) cùng các bản xem trước `policy dry-run` / `policy simulate`.
- **NAT** qua nftables (DNAT và MASQUERADE dưới `nat:` trong cấu hình), áp dụng như một giao dịch `nft -f` duy nhất chỉ chạm vào `table ip goshisen`. Engine chính sách nhìn thấy đích sau-DNAT và nguồn trước-MASQUERADE (`docs/design/architecture.md` §8.2).
- **Giám sát tràn hàng đợi**: metric `goshisen_queue_overflow_total{cause}` cùng cảnh báo giới hạn tần suất. Chỉnh độ sâu bằng `queue_maxlen` (mặc định 1024).
- **Đường nóng không cấp phát** (0 allocs/gói). Benchmark: `docs/design/performance.md`.
- **Đóng gói tái lập được**: các kho lưu trữ tĩnh `linux/amd64` và `linux/arm64` kèm `checksums.txt` và SBOM SPDX (`make package`). Workflow phát hành chỉ tạo bản phát hành dạng draft. Xem `docs/installation.md`.

## OpenWrt

`.apk` SDK-built trong `openwrt/` thêm lớp thực thi NFQUEUE fail-closed **sau** firewall4 trên đường forward; nó không bao giờ thay thế firewall4, không sửa bảng của fw4, và không bao giờ NAT. Cài đặt, tùy chọn UCI, vòng đời, phục hồi và nâng cấp (gồm bản vá upgrade-hook, issue #57): `docs/openwrt.md`. Nhị phân CLI, nâng cấp và hồi lùi: `docs/installation.md`.

## Bắt đầu nhanh

```bash
make build                      # tạo ra bin/goshisen

# Kiểm tra cấu hình và chính sách
./bin/goshisen -c configs/goshisen.yaml config check
./bin/goshisen policy validate --policy-file policies/examples/allow-lab.yaml

# Mô phỏng một gói tin (không thay đổi mạng)
./bin/goshisen policy simulate --policy-file policies/examples/allow-lab.yaml \
  --protocol tcp --state new --src 10.0.0.5 --dst 10.0.0.2 --dest-port 80
#   verdict: ACCEPT
#   matched rule: allow-http

# Chạy trong namespace lab router (root); tường lửa máy chủ không bị động đến
sudo scripts/lab.sh up
sudo ip netns exec gsh_fw ./bin/goshisen \
  --interface gsh-fw1 --policy-file policies/examples/allow-lab.yaml
sudo scripts/lab.sh down
```

## Cấu hình

Thứ tự ưu tiên (thấp đến cao): mặc định, tệp cấu hình, biến môi trường, cờ.

| Khóa | Cờ | Biến môi trường | Mặc định |
| --- | --- | --- | --- |
| `interface` | `--interface` | `GOSHISEN_INTERFACE` | auto (đầu tiên up, không loopback) |
| `policy_file` | `--policy-file` | `GOSHISEN_POLICY_FILE` | *(bắt buộc)* |
| `queue_maxlen` | `--queue-maxlen` | `GOSHISEN_QUEUE_MAXLEN` | `1024` |
| `log_level` | `--log-level` | `GOSHISEN_LOG_LEVEL` | `info` |
| `log_json` | `--log-json` | `GOSHISEN_LOG_JSON` | `false` |

Boolean chấp nhận `true/false`, `1/0`, `yes/no`, `on/off`. Mẫu: `configs/goshisen.yaml`, `policies/`.

## Metrics

Định dạng văn bản Prometheus 0.0.4: `GET /metrics` trên máy chủ API (`127.0.0.1:8080`) hoặc `--metrics-port` riêng (mặc định 9090, chỉ localhost, không xác thực). Lưu ý: trên các bản phát hành ≤ v0.2.6, giá trị tắt `0` chỉ được tôn trọng qua `GOSHISEN_API_PORT=0` / `GOSHISEN_METRICS_PORT=0` (tệp/cờ `0` bị bỏ qua và quay về mặc định); sau v0.2.6 mọi lớp đều tôn trọng `0` tường minh. Chỉ label bản số giới hạn. Danh mục metric và thiết lập Grafana: `docs/release/metrics-grafana-validation.md`.

## Kiểm thử

```bash
go test ./...            # không cần quyền; bộ tích hợp bị bỏ qua khi không có root
sudo make integration    # kịch bản netns thật, cần root (CAP_NET_ADMIN)
```

CI chạy toàn bộ bộ kiểm thử tích hợp trên runner GitHub.

## Tài liệu

| Tài liệu | Nội dung |
| --- | --- |
| [Architecture](docs/design/architecture.md) | Luồng gói, lược đồ chính sách, chế độ lỗi, cấu trúc lab |
| [MVP Scope](docs/design/mvp-scope.md) | Thành phần, kịch bản, tiêu chí chấp nhận, phạm vi loại trừ |
| [User Guide](docs/user-guide.md) | CLI, cấu hình, ví dụ |
| [Developer Guide](docs/developer-guide.md) | Build, test, CI, cấu trúc kho |
| [Installation](docs/installation.md) | Nhị phân CLI, nâng cấp, hồi lùi |
| [OpenWrt](docs/openwrt.md) | Cài gói, UCI, procd, phục hồi |

## Trang tài liệu (`docs-site/`)

Trang tĩnh, không phụ thuộc để xem các tài liệu này, với theme Catppuccin
sáng/tối và công tắc chuyển ngôn ngữ Anh/Việt.

```bash
python3 -m http.server 8901          # phục vụ thư mục gốc của kho, rồi mở:
# http://127.0.0.1:8901/docs-site/index.html
```

- Không bước build, không CDN: việc render markdown được vendored (`docs-site/vendor/md.js`);
  trang hoạt động từ `file://` và mọi máy chủ tĩnh (gồm GitHub Pages).
- Khung giao diện song ngữ (`docs-site/i18n/{en,vi}.json`); nội dung tài liệu đến từ
  markdown của kho, với các bản dịch tiếng Việt đặt trong
  `docs-site/translations/vi/` (cùng đường dẫn tương đối). Bản dịch thiếu sẽ
  quay về tiếng Anh kèm thông báo.
- Manifest thanh bên (`docs-site/manifest.json`) được sinh từ kho; tái tạo sau khi
  thêm/đổi tên tài liệu (xem `docs-site/README.md` để biết lệnh chính xác).

## Giấy phép

MIT, xem [LICENSE](../../../LICENSE). © 2026 Lê Thanh Phong.
