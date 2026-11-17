# GoShiSen-Enforce — Phạm vi MVP

Trạng thái: **Giai đoạn 16 (hiệu suất) đã hoàn tất; Giai đoạn 15 (công bố chính sách nguyên tử) đã hoàn tất; tích hợp đặc quyền Giai đoạn 7 vẫn chưa chạy được trong môi trường này (non-root).** Tài liệu đồng hành với `docs/design/architecture.md`; định nghĩa tường lửa lab MVP là gì, tiêu chí nghiệm thu theo từng giai đoạn, và những gì nằm ngoài phạm vi một cách tường minh.

---

## 1. Mục tiêu MVP

Một tường lửa lab Linux:

- lọc lưu lượng **IPv4 TCP/UDP được chuyển tiếp** giữa hai network namespace,
- đánh giá gói tin bằng **Go** qua **NFQUEUE** (queue 0) với **chính sách YAML khớp đầu tiên**,
- dùng **conntrack kernel** cho trạng thái kết nối (NEW/ESTABLISHED/RELATED/INVALID) và chặn lưu lượng không được theo dõi/không được hỗ trợ,
- giữ **nftables** tối thiểu (forward hook + luật queue, chain policy drop) và dành riêng bảng của nó cho NAT trong tương lai,
- **fail-closed** ngay từ thiết kế (kernel chặn khi không có engine khả dụng),
- chạy hoàn toàn trong một netns lab cô lập, tái lập được với việc thiết lập/dỡ bỏ đáng tin cậy.

Các mục tiêu loại trừ của MVP được liệt kê trong §6; chúng được *dành riêng ở mức thiết kế* (tài liệu architecture nêu tên các điểm mở rộng) nhưng không được triển khai.

---

## 2. Danh mục thành phần (MVP kèm theo gì)

| ID | Thành phần | Nghiệm thu (tối thiểu) |
| --- | --- | --- |
| C1 | Cung cấp ruleset nftables | **Đã hoàn tất (Giai đoạn 3):** daemon tạo `table ip goshisen`, `chain forward` (policy drop), luật queue qua `nft` chỉ trong namespace router. |
| C2 | Bind NFQUEUE | **Đã hoàn tất (Giai đoạn 3):** backend nfnetlink_queue trực tiếp; bind queue 0, đặt maxlen, đọc gói tin, ghi verdicts; bind sau khi nạp chính sách (thứ tự fail-closed). |
| C3 | Parser IPv4/TCP/UDP | **Đã hoàn tất (Giai đoạn 3):** parse không panic; loại bỏ gói cụt/không xác định; mảnh (fragment) và giao thức không được hỗ trợ theo architecture; đã được fuzz. |
| C4 | Engine chính sách | **Đã hoàn tất (Giai đoạn 2):** nạp YAML + xác thực nghiêm ngặt, đánh giá khớp đầu tiên, DROP mặc định, snapshot bất biến, thay thế nguyên tử, validate/simulate ngoại tuyến. Ngữ nghĩa trạng thái/hướng sẽ về trong Giai đoạn 4. |
| C5 | Tích hợp Conntrack | **Đã hoàn tất (Giai đoạn 4):** kernel gắn metadata cho từng gói tin (`NFQA_CT`/`NFQA_CT_INFO`); trạng thái/hướng được ánh xạ sang engine; tập chấp nhận (accepted-set) trong `King`; trạng thái TCP từ protoinfo kernel. Không có bản sao/đăng ký sự kiện conntrack ở userspace (không cần: quyết định dùng metadata CT theo từng gói tin). |
| C6 | Bộ khung lab | **Đã hoàn tất (Giai đoạn 3):** `scripts/lab.sh` up/down/status; tên tài nguyên `gsh_` duy nhất; yêu cầu root; tường lửa máy chủ không bị đụng tới. |
| C7 | Metrics/nhật ký | Prometheus counters (accepted/dropped theo action+protocol) và số liệu tràn hàng đợi; dòng nhật ký kiểm toán cho các luật `log: true`. |
| C8 | Nạp lại | **Đã hoàn tất (Giai đoạn 15):** thay thế snapshot nguyên tử (`policy.King`) được công bố qua một pipeline bền vững dùng chung bởi SIGHUP và API (biên dịch → ghi store+activate → hoán đổi king, trong một khóa). Công bố thất bại/một phần giữ chính sách cũ trong store và ruleset; phục hồi sau sự cố giữa chừng không bao giờ kích hoạt phiên bản rách; metrics kết quả `goshisen_policy_updates_total`; xem trước `policy dry-run`. |

---

## 3. Các lĩnh vực nghiệm thu (xuyên suốt)

Nghiệm thu của mọi giai đoạn triển khai cũng phải thỏa mãn:

- **Fail-closed:** giết daemon làm dừng lưu lượng (các gói tin sau đó bị chặn); khởi động lại khôi phục dịch vụ mà không cần can thiệp thủ công vào nftables.
- **Không bypass:** gói tin không phải TCP/UDP và IPv6 được chuyển tiếp không bao giờ đến được engine (kernel chain policy drop).
- **Tất định:** chính sách giống nhau + luồng gói tin giống nhau → verdicts giống nhau giữa các lần chạy.
- **Dọn dẹp:** sau khi dỡ bỏ `scripts/lab.sh`, `ip netns list` và `ip link show` không hiển thị artifact lab nào còn sót lại; lần chạy thứ hai hoạt động mà không cần can thiệp thủ công.
- **Build:** `go build ./...`, `go vet ./...`, và `go test ./...` đạt từ một bản checkout sạch.

---

## 4. Kịch bản end-to-end (nghiệm thu chức năng)

Mỗi kịch bản chạy trên cấu trúc liên kết lab (`fw`/`client`/`server`, §7 của architecture.md); một kịch bản đạt khi probe được nêu tên tạo ra kết quả được nêu tên.

| # | Kịch bản | Chính sách đang kiểm tra (khái niệm) | Probe | Kỳ vọng |
| --- | --- | --- | --- | --- |
| S1 | DROP mặc định | ruleset rỗng | client→server TCP/UDP | tất cả bị chặn |
| S2 | Cho phép tường minh + trạng thái | `allow-new` cho dst_port 80 TCP, `allow-established` cho ESTABLISHED/RELATED | client→server TCP 80, sau đó trả lời | bắt tay hoàn tất; các phản hồi đi qua |
| S3 | Phản hồi không có accept | có `allow-established`, **không** có NEW-allow | server→client TCP SYN không được yêu cầu | bị chặn (không có kết nối được chấp nhận) |
| S4 | Khớp đầu tiên | hai luật chồng lấn trên dst_port 8080 (DROP trước, ACCEPT sau) | client→server TCP 8080 | bị chặn (khớp đầu tiên thắng). Được kiểm tra ngoại tuyến bằng `policy simulate`. |
| S5 | Ngữ nghĩa CIDR + cổng | chỉ cho phép `10.0.0.0/24`→server:22 | (a) nguồn trong mạng con, (b) nguồn khác | (a) được cho phép, (b) bị chặn |
| S6 | Lọc giao thức | chỉ cho phép UDP 53 | TCP 53 vs UDP 53 | TCP bị chặn, UDP được cho phép |
| S7 | INVALID | luật state:INVALID DROP | gói tin sai dạng/không được theo dõi (ví dụ: mảnh muộn) | bị chặn |
| S8 | IPv6/non-TCP-UDP | mọi chính sách | probe IPv6 TCP, ICMP, GRE | bị chặn (không bao giờ vào hàng đợi) |
| S9 | Nạp lại | cho phép TCP 22 → nạp lại để cho phép TCP 2222 | probe 22 sau khi nạp lại | bị chặn; chính sách mới chi phối; lưu lượng cho các kết nối established hiện có tiếp tục |
| S10 | Sự cố/khởi động lại | cho phép TCP 80 | giết daemon giữa kết nối, probe; khởi động lại, probe | probe thất bại khi daemon tắt; sau khi khởi động lại, các kết nối mới hoạt động |
| S11 | Tràn hàng đợi | làm tràn vượt quá maxlen | counter tràn tăng; không có bypass chính sách (gói tin bị chặn vẫn bị chặn) |
| S12 | Chính sách không hợp lệ | YAML sai / cổng sai / CIDR sai | daemon từ chối khởi động (hoặc giữ chính sách cũ khi nạp lại) kèm lỗi rõ ràng |

---

## 5. Tiêu chí nghiệm thu theo giai đoạn

### Giai đoạn 1 — Nền tảng (HOÀN TẤT)
- [x] Mô-đun Go hợp lệ; `go build ./...`, `go vet ./...`, `go test ./...` đạt
- [x] CLI: `--help`, `--version`, `--config/-c`, `--interface`, `--policy-file`, `--log-level`, `--log-json`; cờ/lệnh không xác định báo lỗi với mã thoát 2
- [x] Thứ tự ưu tiên cấu hình defaults < file < env (GOSHISEN_*) < flags; nguồn của từng trường được ghi lại và hiển thị bởi `config show`
- [x] Giải mã tệp YAML nghiêm ngặt (khóa không xác định bị loại bỏ); lỗi rõ ràng nêu tên trường, giá trị, và cách sửa
- [x] Ghi nhật ký có cấu trúc (`log/slog`, text hoặc JSON) tôn trọng `log_level`
- [x] Xử lý tín hiệu: SIGINT/SIGTERM → tắt máy nhẹ nhàng, thoát 0
- [x] Không sửa đổi hệ thống: chỉ xác thực chỉ-đọc (tệp chính sách đọc được, interface tồn tại/ở trạng thái up)
- [x] Cổng Makefile (`make check` = fmt + vet + test + build) và workflow CI
- [x] Cấu hình mẫu `configs/goshisen.yaml`; tài liệu (README, hướng dẫn người dùng/nhà phát triển) khớp với kho

### Giai đoạn 2 — Engine chính sách (HOÀN TẤT)
- [x] Biên dịch chính sách YAML với giải mã nghiêm ngặt (trường không xác định bị loại bỏ; `state`/`in_iface`/`out_iface` dành riêng bị loại bỏ kèm thông báo cụ thể)
- [x] Bộ khớp IPv4 CIDR (`any`, `a.b.c.d/n`, host trần) và cổng (đơn, bao gồm `lo:hi`), giao thức TCP/UDP/any
- [x] Hành động ACCEPT/DROP; ID luật duy nhất (trùng lặp bị loại bỏ)
- [x] Đánh giá khớp đầu tiên và DROP mặc định; tổ hợp cổng/giao thức mơ hồ bị loại bỏ
- [x] `Snapshot` bất biến (bản sao phòng thủ) và thay thế `King` nguyên tử (an toàn đồng thời, đã test `-race`)
- [x] Metadata `policy.Packet` độc lập backend; engine chạy không cần root hay mạng
- [x] Test theo bảng: ranh giới CIDR, ranh giới cổng, luật chồng lấn, thứ tự luật, từ chối mặc định, cấu hình không hợp lệ, nạp lại/hoán đổi nguyên tử
- [x] Mô phỏng ngoại tuyến: `goshisen policy validate` và `goshisen policy simulate` (verdict + ID luật khớp; không thay đổi hệ thống)

### Giai đoạn 3 — Thực thi gói tin thật (HOÀN TẤT; chờ xác minh lab)
- [x] Backend NFQUEUE (`internal/packet/capture.go`) nói chuyện nfnetlink_queue qua `github.com/mdlayher/netlink` (trực tiếp; không cgo; `go-nfnetlink` không tồn tại như một mô-đun)
- [x] Mọi gói tin vào hàng đợi đều nhận được verdict (lỗi parse, thiếu decider, hoặc panic engine → DROP; phục hồi panic đã được test)
- [x] Parser IPv4/TCP/UDP: không bao giờ panic trên dữ liệu vào cụt/sai dạng/không được hỗ trợ; đã fuzz (`FuzzParse`, 3M lần thực thi sạch); hành vi mảnh và giao thức không được hỗ trợ theo architecture §6.2–§6.3
- [x] Hook nftables (`internal/hook`): luật `ip goshisen` + forward chain (policy drop) + luật queue; chỉ được tạo trong namespace router; tường lửa máy chủ không bao giờ bị đụng tới
- [x] Lab cô lập `scripts/lab.sh`: namespaces/veths `gsh_` duy nhất, up/down/status, yêu cầu root, dọn dẹp tất định
- [x] Test tích hợp (lưu lượng thật, không mock): TCP được cho phép, UDP được cho phép, lưu lượng bị từ chối, DROP mặc định, queue-không-có-listener, tắt máy sạch — cả sáu đều có mặt; **yêu cầu đặc quyền** (`checkPrereqs`), bị bỏ qua ở đây kèm lý do tường minh vì máy này chạy với tư cách non-root
- [x] Kết nối daemon: chính sách được nạp trước khi cài hook; fail-closed ở mọi bước; tắt máy SIGINT/SIGTERM nhẹ nhàng kèm tóm tắt counter
- [ ] Nghiệm thu: chạy `sudo make integration` trên host/runner đặc quyền để thực thi sáu kịch bản (không thể thỏa mãn trong phiên này — không có root)

### Giai đoạn 4 — Lọc có trạng thái (HOÀN TẤT; chờ xác minh lab)
- [x] Đã xác minh backend phơi bày metadata cần thiết: cờ kernel `NFQA_CFG_F_CONNTRACK` được xác nhận trong headers; `NFQA_CT` + `NFQA_CT_INFO` được gắn cho từng gói tin; `IP_CT_IS_REPLY` cho hướng tường minh; `CTA_PROTOINFO_TCP_STATE` cho trạng thái TCP kernel (không tái triển khai conntrack bằng Go)
- [x] Backend được cấu hình để yêu cầu thuộc tính CONNTRACK; `applyCTInfo` ánh xạ ctinfo kernel → trạng thái/hướng engine; không được theo dõi → INVALID
- [x] Bộ khớp `state` tồn tại trong biên dịch chính sách (NEW/ESTABLISHED/RELATED/INVALID; giá trị không hợp lệ bị loại bỏ)
- [x] Ngữ nghĩa có trạng thái: NEW-accept ghi kết nối vào accepted-set của `King`; ESTABLISHED/RELATED yêu cầu được chấp nhận (riêng five-tuple không bao giờ chứng minh được trạng thái established); hướng tường minh
- [x] Nạp lại chính sách (SIGHUP) nguyên tử; nạp lại thất bại giữ chính sách cũ; kết nối được chấp nhận sống sót qua nạp lại (phủ engine + tích hợp)
- [x] Đã thêm test tích hợp (tất cả yêu cầu root, bị bỏ qua ở đây kèm lý do): TCP handshake+echo, inbound không được yêu cầu, trạng thái kết nối không hợp lệ, connection timeout/hold, UDP timeout/reuse, reload-while-established
- [x] Hạn chế ICMP RELATED đã được ghi lại (hook chỉ đưa TCP/UDP vào hàng đợi; lỗi ICMP không bao giờ đến engine)
- [ ] Nghiệm thu: chạy `sudo make integration` trên host/runner đặc quyền để thực thi các kịch bản trạng thái (không thể thỏa mãn trong phiên này — không có root)

### Giai đoạn 5 — REST API + metrics (HOÀN TẤT)
- [x] REST API `internal/api`: bind 127.0.0.1 theo mặc định; xác thực bearer-token cho thao tác quản trị (chỉ config/env, không bao giờ argv); body ≤16 KiB (413); server timeouts; lỗi JSON có cấu trúc
- [x] Xem chính sách đang hoạt động (`GET /v1/policy`); validate mà không áp dụng (`POST /v1/policy/validate`); áp dụng theo phiên bản với `base_version` compare-and-swap (ghi đồng thời không tương thích → 409, không bao giờ ghi đè âm thầm); liệt kê phiên bản; kích hoạt phiên bản đã lưu (rollback)
- [x] Chính sách không hợp lệ → 422 trước mọi thay đổi trạng thái; chính sách đang hoạt động được giữ nguyên (đã unit-test)
- [x] Kho chính sách bền vững `internal/store`: `<version>.yaml` bất biến + con trỏ `active.json` nguyên tử (temp+fsync+rename+dir-fsync); phục hồi khi khởi động lại; rollback theo phiên bản
- [x] Nhật ký kiểm toán cho thao tác quản trị + phiên bản; token không bao giờ được ghi lại (cũng bị redact trong `config show`)
- [x] Metrics `internal/metrics`: label cardinality giới hạn (enum action/protocol/state, loại lỗi), gauge active-policy-version, histogram verdict-latency; `metrics_port` độc lập + `GET /metrics` trên API; không có label IP/port/flow-ID
- [x] Liveness `GET /healthz`; readiness `GET /readyz` (phản ánh trạng thái thực thi)
- [x] `api/openapi.yaml` khớp với triển khai
- [x] Các test: auth (thiếu/hợp lệ + audit), xung đột, invalid-preserves-active, áp dụng đồng thời (chính xác một người thắng), phục hồi khởi động lại, activate/rollback, giới hạn body, readiness, metrics, tính nguyên tử của store; tất cả `-race` sạch
- [x] Cấu hình: `api_host`, `api_port`, `api_token`, `metrics_port`, `policy_dir` (+ env, file; flags trừ token)
- [ ] Tích hợp đặc quyền: API + metrics được vận hành bên trong lab (yêu cầu root; không chạy được trong môi trường này)

### Giai đoạn 6 — NAT + đóng gói (HOÀN TẤT; chờ xác minh lab)
- [x] NAT hoàn toàn phía kernel: chain nftables `dstnat` (DNAT) + `srcnat` (MASQUERADE) trong bảng do ứng dụng sở hữu; không ghi lại địa chỉ/checksum bằng Go
- [x] Áp dụng nguyên tử: một giao dịch `nft -f` cho mỗi lần cài (`add table` + `delete table` + định nghĩa lại đầy đủ trong một batch); áp dụng thất bại không thay đổi gì; `Cleanup` (và lệnh `goshisen hook cleanup`) chỉ xóa bảng của ứng dụng; không flush ngoài phạm vi
- [x] Ngữ nghĩa địa chỉ đã được ghi lại: engine thấy dst sau DNAT (prerouting diễn ra trước FORWARD) và src trước MASQUERADE (postrouting theo sau FORWARD); lưu lượng trả về nhờ conntrack; được chứng minh bằng test tích hợp
- [x] Cấu hình: `nat.enabled`, `nat.masquerade.{enabled,out_iface}`, `nat.dnat[]` (name/protocol/pub_ip/pub_port/target_ip/target_port) kèm xác thực
- [x] Đã thêm test tích hợp (tất cả yêu cầu root, bị bỏ qua ở đây kèm lý do): DNAT, MASQUERADE, lưu lượng trả về, NAT+lọc (góc nhìn sau-DNAT), reload-while-NAT-connections (hold probe sống sót qua SIGHUP)
- [x] Dockerfile: multi-stage (build Go + build nftables tĩnh → base distroless), image runtime tối thiểu, `--cap-add=NET_ADMIN,NET_RAW` tường minh, không có chế độ privileged, hướng dẫn health-check (healthz/readyz), tắt máy SIGTERM nhẹ nhàng (STOPSIGNAL TERM), volume chính sách bền vững, hướng dẫn khởi động lại/phục hồi
- [x] Không có script cài đặt (chưa đạt tiêu chuẩn độ tin cậy uninstall/rollback; đã ghi lại); không có manifest Kubernetes (quan hệ CNI chưa xác định; đã ghi lại)
- [ ] Nghiệm thu: chạy `sudo make integration` trên host/runner đặc quyền để thực thi các kịch bản NAT (không thể thỏa mãn trong phiên này — không có root)

### Giai đoạn 7 — Tăng cường cứng/phát hành
- [x] Độ bền vững: xử lý tràn hàng đợi + counters (S11) — số liệu drop kernel qua `/proc/net/netfilter/nfnetlink_queue`, metric `goshisen_queue_overflow_total` có giới hạn, nhật ký cảnh báo giới hạn tốc độ, `queue_maxlen` cấu hình được; bất biến fail-closed được neo bằng unit test (không bao giờ yêu cầu `NFQA_CFG_F_FAIL_OPEN`). Phủ tích hợp trong `tests/integration/overflow_test.go` (yêu cầu root; bị bỏ qua ở đây vì non-root).
- [x] Độ bền vững: gia cố sự cố/khởi động lại (S10) — readiness gắn với đường thực thi (hook + queue đã bind; bị xóa khi tắt máy), phủ tích hợp SIGKILL fail-closed + phục hồi (`tests/integration/crash_test.go`, yêu cầu root), ví dụ supervisor (unit systemd `deployments/systemd/goshisen.service`, vòng lặp khởi động lại lab `scripts/supervisor.sh`). Độ tin cậy dỡ bỏ lab dưới `kill -9` trong lúc có lưu lượng (D4) được vận hành bởi bước dỡ bỏ của crash test.
- [ ] Ghim phụ thuộc trong go.mod, `go mod verify` sạch, `go vet`/`go test ./...` xanh (D5).
- [ ] Chốt tài liệu: hướng dẫn người dùng (cấu hình, chạy, lab), hướng dẫn nhà phát triển (build, test, đóng góp), SECURITY.md, trạng thái README chính xác.
- [ ] Nghiệm thu cuối cùng: bộ kịch bản đầy đủ + dọn dẹp lab + build từ bản clone nguội.

### Giai đoạn 14 — Chính sách Flow-offload (Issue #6)
- [x] Cấu hình: `offload_policy` (`warn` mặc định | `refuse` | `readiness`) được xác thực khi nạp; nguồn file/env kèm thứ tự ưu tiên được ghi lại; không có flag (hiển thị trong `config show`).
- [x] `refuse`: khởi động thất bại với lỗi có thể hành động TRƯỚC khi cài hook khi phát hiện offloading — fail-closed nhờ vắng mặt, hệ thống không bị đụng tới (unit test: hook/queue không bao giờ bị đụng tới).
- [x] `readiness`: daemon vẫn thực thi nhưng không bao giờ trở thành ready khi offloading đang hoạt động; watcher 10 giây gỡ bỏ readiness khi offloading xuất hiện muộn hơn và khôi phục nó khi ruleset sạch (unit test cho cả hai cạnh).
- [x] `warn` (mặc định): hành vi không đổi — nhật ký WARN + gauge `goshisen_flow_offload_active`.
- [x] Gói OpenWrt: tùy chọn UCI `offload_policy` được init script xác thực và đưa vào YAML daemon; test hồi quy trong `tests/package`.
- [x] Tài liệu: hướng dẫn người dùng, `docs/openwrt.md` (bảng UCI + các khoảng trống thực thi), SECURITY.md, architecture §9, trạng thái mvp-scope được đồng bộ.

### Giai đoạn 15 — Công bố chính sách nguyên tử
- [x] Pipeline công bố thống nhất dùng chung bởi nạp lại SIGHUP và apply/activate REST API: biên dịch → phiên bản tiếp theo từ store bền vững → ghi bền vững (chính sách rồi đến con trỏ active) → hoán đổi king, tất cả dưới một khóa đột biến toàn tiến trình. Không chính sách nào bao giờ hiển thị ở trạng thái áp dụng nửa chừng; mọi thất bại giữ chính sách trước đó trong cả store lẫn ruleset sống.
- [x] Phục hồi fail-closed giữa chừng công bố: phiên bản đã ghi nhưng chưa kích hoạt không bao giờ trở thành active sau khi khởi động lại daemon (phục hồi đọc con trỏ active).
- [x] Metrics: `goshisen_policy_updates_total{outcome="success"|"failure"|"conflict"}`; `goshisen_active_policy_version` phản ánh phiên bản active của store sau phục hồi.
- [x] `policy dry-run --policy-file F --policy-dir D`: xác thực F và xem trước đúng những gì sẽ công bố (phiên bản tiếp theo, chênh lệch luật, thay đổi hành động mặc định) mà không ghi; thiếu store là lỗi (không bao giờ được tạo).
- [x] Các test: YAML sai dạng / chính sách không hợp lệ giữ ruleset cũ; công bố đồng thời được tuần tự hóa thành các phiên bản riêng biệt với sự thống nhất store/king; xung đột CAS trên base cũ; lỗi store một phần giữ chính sách cũ; chính sách IPv4+IPv6 được công bố và quyết định đúng; nạp lại SIGHUP end-to-end khi đang chạy; phục hồi khởi động lại sau một lần giết mô phỏng giữa chừng công bố.

### Giai đoạn 16 — Hiệu suất
- [x] Bộ benchmark đã được commit: parser (`internal/packet/parser_bench_test.go`), engine (`internal/policy/engine_bench_test.go`), pipeline quyết định đầy đủ gồm cả metrics (`internal/packet/pipeline_bench_test.go` — được bảo vệ bởi `TestPipelineRuns` chống lại đường chết).
- [x] Đã ghi lại baseline trước tối ưu trên cùng máy rảnh (3 lần chạy, median + σ), profile cho thấy `fmt.doPrintf` ≈ 55% CPU trong `King.Process` (accepted-set khóa chuỗi).
- [x] Đã tối ưu đường nóng: khóa 5-tuple đóng gói không cấp phát (`tupleKey` struct, không `fmt`/chuỗi), `sync.RWMutex`+map accepted-set, `Rule` gọn nhẹ (state bitmask + value ports), metrics gói tin mảng cố định một khóa (`ObservePacket`), pooled verdict buffer, parse thuộc tính inline.
- [x] Kết quả (median, cùng môi trường, 3 lần chạy): King.Process NEW accept 934→103 ns (−89%), ESTABLISHED 409→53 ns (−87%), tupleKey 359→4.4 ns (−99%), pipeline e2e NEW accept 1129→253 ns (−78%), established reply 634→193 ns (−70%), default-drop 668→206 ns (−69%), metrics-only 86.5→15.8 ns (−82%), latency p50 1.04→0.31 µs, p95 1.72→0.31 µs, p99 2.69→0.48 µs; **0 lần cấp phát cho mỗi gói tin** (trước đây là 15–16).
- [x] Ngữ nghĩa được giữ nguyên: chuẩn hóa phản hồi có trạng thái được neo bởi `internal/policy/tuplekey_test.go` (v4/v6); `go test -race ./...` và `make check` xanh; định dạng verdict trên wire không đổi (`TestVerdictMessageGolden`).
- [x] Tài liệu: `docs/design/performance.md` (baseline/profile/kết quả, env, phương sai), architecture §9 hàng 16, trạng thái mvp-scope được đồng bộ. Số lượng worker không đổi (2 goroutines, mỗi cái một queue socket v4/v6); độ co giãn theo kích thước chính sách đã được xác minh (trường hợp xấu nhất 200 luật).

---

## 6. Nằm ngoài phạm vi MVP một cách tường minh (dành riêng)

| Tính năng | Vì sao chưa làm | Hook thiết kế (sẽ đi vào đâu) |
| --- | --- | --- |
| Lọc IPv6 | MVP là IPv4 TCP/UDP | bảng nftables `ip6` hoặc family inet; parser header IPv6 |
| ICMP/giao thức L4 khác | không nằm trong lưu lượng mục tiêu | bộ khớp luật queue; mở rộng parser; enum giao thức engine |
| DNAT/MASQUERADE | **đã triển khai trong Giai đoạn 6** (nftables phía kernel) | xem §8.2 architecture.md để hiểu ngữ nghĩa trước/sau NAT |
| Chuyển tiếp cổng ngoài các dạng DNAT trên | DNAT đáp ứng nhu cầu MVP | các dạng luật `dnat` bổ sung |
| Verdicts REJECT | MVP chỉ chặn | luật `reject` nftables (phía kernel), không phải NFQUEUE |
| REST API / quản lý luật động | **đã triển khai trong Giai đoạn 5** | `internal/api` |
| eBPF/XDP offload | cơ chế hoàn toàn khác | n/a |
| Multi-queue / queue-balance, GSO offload | tinh chỉnh hiệu suất ngoài nhu cầu lab | NFQUEUE `queue-balance`, `NFQA_CFG_F_GSO` |
| TLS/mã hóa mặt điều khiển | quản lý là netns cục bộ | ngoài phạm vi |
| Khả dụng cao, cụm, nhân bản trạng thái | chỉ tường lửa lab | n/a |
| Manifest triển khai Kubernetes | quan hệ với CNI của cụm chưa được xác định (tường lửa pod-CNI sẽ xung đột hoặc trùng lặp với các luật NAT/lọc của chính CNI); phải được xác định trước khi tồn tại manifest | ghi lại mô hình tương tác CNI trước |
| Script cài đặt/gỡ cài đặt | cần ngữ nghĩa gỡ cài đặt + rollback đáng tin cậy thì mới đáng phát hành; chưa đạt được | chỉ phát hành kèm bước dỡ bỏ đã xác minh |
| Web UI, cảnh báo Prometheus, bảng điều khiển | giám sát ngoài counters | không có kế hoạch; không tồn tại cây web trong kho |
| QoS/giới hạn tốc độ/định hình lưu lượng | không được yêu cầu bởi mô hình chính sách | meters/limits nftables |
| Pipeline ghi nhật ký/kiểm toán ngoài dòng `log: true` | tối thiểu | mở rộng `internal/metrics` |

---

## 7. Ghim phụ thuộc / rủi ro tương thích (tóm tắt)

Xem `docs/design/architecture.md` §8 để có ma trận đầy đủ. MVP ghim:

- `github.com/mdlayher/netlink` (ghim v1.11.2) — nền tảng của backend NFQUEUE; `go-nfnetlink` không tồn tại và đã bị bỏ rơi.
- `gopkg.in/yaml.v3` — giải mã chính sách nghiêm ngặt.
- Parser IPv4/TCP/UDP tự viết (không dùng gopacket), đã fuzz.
- Cung cấp hook qua nhị phân `nft` (công cụ host), không phải thư viện.
- Mô-đun kernel `nf_tables`, `nfnetlink_queue`, `nf_conntrack`, `nf_defrag_ipv4`; từ chối khởi động kèm chẩn đoán rõ ràng nếu thiếu.
- Go ≥ 1.25 (go.mod ghim 1.25.0), Linux ≥ 5.6.

---

## 8. Định nghĩa hoàn thành (toàn bộ Giai đoạn 1–7)

- Tường lửa hoạt động trong lab như quy định trong §4 (tất cả kịch bản S1–S12 được ghi lại ở trên),
- architecture.md + tài liệu này chính xác, README phản ánh trạng thái thật tại mọi ranh giới giai đoạn,
- test cho mọi tiêu chí nghiệm thu được liệt kê ở trên là một phần của `go test ./...` hoặc script lab (không có nghiệm thu không thể xác minh),
- không có TODO đã biết trong mã đã commit; mọi tính năng dành riêng được ghi lại là ngoài phạm vi ở đây.

### Giai đoạn 17 — Phát hiện + vô hiệu hóa Offload (backlog #2)
- [x] Máy trạng thái phát hiện: `disabled` / `enabled` / `failed` / `unknown` (parse ruleset + fallback flowtables; unknown = không thể chứng minh an toàn).
- [x] `offload_policy=enforce`: chủ động vô hiệu hóa trạng thái flow-offload (flowtables + luật flow-offload, một giao dịch nft, được xác minh bằng phát hiện lại); fail-closed khi khởi động nếu vô hiệu hóa thất bại hoặc trạng thái unknown.
- [x] `allow_bypass` cửa thoát hiểm chẩn đoán/phát triển (chỉ enforce; hạ cấp xuống warn kèm cảnh báo ồn ào; không bao giờ là mặc định, không bao giờ âm thầm).
- [x] CLI: `goshisen offload status` (chỉ-đọc, thoát 0) và `goshisen offload disable` (idempotent, đã xác minh, thoát khác 0 khi thất bại).
- [x] Watchdog: phát hiện lại mỗi 10 giây; enforce thử lại vô hiệu hóa nếu một offload xuất hiện sau khi khởi động.
- [x] Metrics: `goshisen_flow_offload_state` (0/1/2) + `goshisen_flow_offload_disables_total{result=success|failed}`.
- [x] Gói OpenWrt: UCI `offload_policy` chấp nhận enforce; tùy chọn UCI `allow_bypass` được đưa ra; test trong `tests/package`.
- [x] Tài liệu: `docs/design/offload-disable.md`, `docs/openwrt.md`, architecture §9.

### Giai đoạn 18 — Khả năng quan sát bypass Flowtable (backlog #3)
- [x] Hợp đồng đo lường: `counter` flowtable là nguồn hit quan sát được; chỉ parse chỉ-đọc `nft -a list ruleset`.
- [x] Mô hình trạng thái: absent / present_zero_hit / present_positive_hit / unknown; zero hits ≠ offload đã bị vô hiệu hóa (không bao giờ suy diễn).
- [x] CLI `offload observe`: timestamp, trạng thái, số hit tích lũy theo từng flowtable, độ tin cậy (high/limited/unknown) + lý do.
- [x] Metrics: `goshisen_flowtable_hits_packets` (gauge, -1 không khả dụng) + `goshisen_flowtable_hits_available`; watchdog/khởi động ghi chúng.
- [x] Xử lý reset/tràn counter (DeltaHits): sau < trước → báo cáo giá trị hiện tại, không có delta âm.
- [x] Các test: parser (counter có/trần/không có/sai dạng), delta (dương/reset/không), observe (absent/unknown/positive/limited), metrics.
- [x] Tài liệu: `docs/design/offload-observability.md` (hợp đồng, mô hình trạng thái, reset, giới hạn nền tảng, an toàn).
