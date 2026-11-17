# GoShiSen-Enforce — Hướng dẫn người dùng

Trạng thái: **hardening/release.** Hướng dẫn này mô tả daemon như đã build: CLI, cấu hình, vòng đời, engine chính sách, lọc có trạng thái, REST API, metrics, NAT, và giám sát tràn hàng đợi.

## Build

```bash
make build
```

Tạo ra `bin/goshisen`. Phiên bản của nhị phân lấy từ `git describe` (fallback `dev`); ghi đè bằng `make build VERSION=1.2.3`.

## Dòng lệnh

```
goshisen [flags] [command]
```

| Cờ | Mô tả |
| --- | --- |
| `-h`, `--help` | in trợ giúp và thoát |
| `-v`, `--version` | in phiên bản và thoát |
| `-c`, `--config <path>` | tệp cấu hình YAML |
| `-i`, `--interface <name>` | giao diện để dùng (ghi đè file/env) |
| `--policy-file <path>` | đường dẫn tới tệp chính sách YAML (ghi đè file/env) |
| `--log-level <lvl>` | `debug` \| `info` \| `warn` \| `error` |
| `--log-json` | phát log JSON có cấu trúc |
| `--queue-maxlen <n>` | độ dài hàng đợi NFQUEUE kernel tính theo gói (mặc định 1024; phạm vi 1–65535) |

Lệnh:

| Lệnh | Hành vi |
| --- | --- |
| `config show` | in cấu hình đã giải quyết, kèm nguồn ưu tiên (`default`/`file`/`env`/`flag`) của từng giá trị |
| `config check` | kiểm tra cấu hình và máy chủ chỉ-đọc: tệp chính sách đọc được, giao diện tồn tại và up. Không thay đổi hệ thống; mã thoát 0 khi thành công |
| `policy validate --policy-file <path>` | biên dịch tệp chính sách, từ chối quy tắc không hợp lệ, in tóm tắt các quy tắc đã biên dịch. Không thay đổi hệ thống |
| `policy simulate --policy-file <path> --src <ip> --dst <ip> [--protocol tcp\|udp\|any] [--state new\|established\|related\|invalid] [--source-port <n>] [--dest-port <n>]` | đánh giá một gói tin tổng hợp dựa trên chính sách và in verdict cùng ID quy tắc khớp (hoặc `(default)`). Quy tắc có trạng thái yêu cầu `--state`; ngược lại gói được xem là không trạng thái và quy tắc có trạng thái không khớp. Không thay đổi hệ thống |
| `policy dry-run --policy-file <path> --policy-dir <dir>` | kiểm tra chính sách và xem trước chính xác việc công bố mà daemon sẽ thực hiện lên kho chính sách bền vững: phiên bản kế tiếp, delta quy tắc so với chính sách đang hoạt động, và thay đổi hành động mặc định. Đọc kho, không ghi gì; thiếu kho là lỗi (không bao giờ tạo ra) |
| `hook status` | báo cáo liệu bảng nftables do ứng dụng sở hữu (`ip goshisen`) có được cài đặt không. Chỉ đọc; không thay đổi gì |
| `hook cleanup` | xoá chỉ bảng nftables do ứng dụng sở hữu (`ip goshisen`). Cần root (CAP_NET_ADMIN); không bao giờ động vào trạng thái nftables khác |
| *(không)* | chạy daemon |

Cờ phải đứng trước lệnh: `goshisen -c f.yaml config check`. Cờ thuộc lệnh con nằm **sau** lệnh con: `goshisen policy simulate --src 1.2.3.4 --dst 5.6.7.8`.

## Cấu hình

Mẫu: `configs/goshisen.yaml`.

```yaml
interface: eth0          # rỗng/bỏ qua = auto (đầu tiên up, không loopback)
policy_file: policies/default/deny-all.yaml   # bắt buộc
log_level: info          # debug | info | warn | error
log_json: false          # true = log JSON, false = văn bản
queue_maxlen: 1024       # độ dài hàng đợi NFQUEUE kernel (1..65535)
offload_policy: warn     # warn | refuse | readiness (xem bên dưới)
```

Khóa không biết bị từ chối khi nạp. `~` trong `policy_file` được mở rộng tới thư mục nhà của người dùng.

### Biến môi trường

Tất cả dùng tiền tố `GOSHISEN_`; chúng ghi đè giá trị file nhưng không ghi đè cờ.

| Biến | Cờ tương đương |
| --- | --- |
| `GOSHISEN_INTERFACE` | `--interface` |
| `GOSHISEN_POLICY_FILE` | `--policy-file` |
| `GOSHISEN_LOG_LEVEL` | `--log-level` |
| `GOSHISEN_LOG_JSON` | `--log-json` |
| `GOSHISEN_QUEUE_MAXLEN` | `--queue-maxlen` |
| `GOSHISEN_OFFLOAD_POLICY` | *(không cờ; chỉ qua file hoặc env)* |

Giá trị boolean chấp nhận `true/false`, `1/0`, `yes/no`, `on/off`. Giá trị env không hợp lệ là lỗi khi khởi động.

### Thứ tự ưu tiên

```
mặc định tích hợp < tệp cấu hình < môi trường < cờ
```

`config show` là cách có thẩm quyền để xem cấu hình hiệu lực và từng giá trị đến từ đâu.

## Chạy daemon

Daemon thực thi lưu lượng trong **namespace mạng hiện tại**. Khi dùng bình thường, nó chạy bên trong namespace lab router (`gsh_fw`), nên mọi thay đổi nftables đều được cô lập:

```bash
sudo scripts/lab.sh up
sudo ip netns exec gsh_fw ./bin/goshisen \
  --interface gsh-fw1 --policy-file policies/examples/allow-lab.yaml
sudo scripts/lab.sh down
```

- Thứ tự khởi động: giải quyết cấu hình → biên dịch chính sách (chính sách không hợp lệ làm hủy bỏ trước mọi thay đổi) → kiểm tra trước flow-offload (`offload_policy: refuse` hủy bỏ tại đây, hook không bao giờ được cài) → cài hook nftables (chỉ trong namespace router) → bind NFQUEUE → thực thi.
- Mọi gói được xếp hàng đều nhận verdict. Lỗi parse, lỗi engine, và panic đều fail closed (DROP).
- `SIGINT`/`SIGTERM` dừng hàng đợi và thoát sạch (mã thoát 0). Hook vẫn ở nguyên vị trí — không có listener, kernel sẽ DROP (fail-closed) cho đến khi daemon quay lại.
- **Hành vi crash:** nếu daemon chết đột ngột (`SIGKILL`), hook vẫn nằm trong kernel và hàng đợi không có listener, nên lưu lượng bị DROP (fail-closed). Khởi động lại daemon cài lại hook và bind lại hàng đợi; chính sách được khôi phục từ kho bền vững / tệp chính sách.
- **Giám sát:** hai lựa chọn được cung cấp. `deployments/systemd/goshisen.service` chạy daemon trong namespace lab router dưới systemd với `Restart=always` (tự phục hồi khi crash). `scripts/supervisor.sh` là vòng lặp khởi động lại không phụ thuộc cho lab: `sudo scripts/lab.sh up && sudo scripts/supervisor.sh`. Giữ `--stop-timeout`/`TimeoutStopSec` ở ≥ cửa sổ drain sạch (daemon drain hàng đợi khi TERM).
- **Sẵn sàng:** `GET /readyz` chỉ trả 200 khi đường thực thi đã hoàn toàn up (hook nftables đã cài *và* NFQUEUE đã bind); nó trả 503 trong lúc khởi động, khi hook/queue lỗi, và từ lúc bắt đầu tắt máy. Với `offload_policy: readiness`, API giữ 503 khi flow offloading đang hoạt động (xem "Nhận thức flow-offload").
- **Tường lửa máy chủ không bao giờ bị động đến.** Hook chỉ chạm vào `table ip goshisen` trong namespace hiện tại; teardown xoá các namespace và mọi thứ bên trong.

Kiểm thử tích hợp dùng lưu lượng thật trên cùng đường; chúng yêu cầu root:

```bash
sudo make integration
```

## Mã thoát

| Mã | Ý nghĩa |
| --- | --- |
| 0 | thoát sạch (help/version/check ok/đóng máy mượt) |
| 1 | lỗi xác thực cấu hình hoặc máy chủ, hoặc lỗi runtime |
| 2 | lỗi dùng CLI (cờ/lệnh không biết) |

## NAT

Cấu hình dưới `nat:` trong cấu hình daemon (nftables phía kernel; không viết lại địa chỉ/checksum trong Go):

```yaml
nat:
  enabled: true
  masquerade:
    enabled: true
    out_iface: eth1
  dnat:
    - name: web
      protocol: tcp
      pub_ip: 203.0.113.10
      pub_port: 80
      target_ip: 10.0.0.10
      target_port: 8080
```

- Toàn bộ ruleset (filter + NAT) được áp dụng như một giao dịch; một lần áp dụng lỗi không thay đổi gì, và chỉ bảng `table ip goshisen` do ứng dụng sở hữu bị động đến.
- **Đánh giá chính sách nhìn thấy đích sau-DNAT** (prerouting chạy trước hook filter) và **nguồn trước-MASQUERADE** (postrouting chạy sau). Quy tắc phải khớp địa chỉ đích nội bộ.
- Lưu lượng trả về được conntrack bỏ NAT và xử lý bởi quy tắc ESTABLISHED/RELATED.

## REST API

Daemon lộ API quản lý (mặc định `127.0.0.1:8080`). Xem `api/openapi.yaml` để biết đặc tả đầy đủ.

| Endpoint | Auth | Mục đích |
| --- | --- | --- |
| `GET /v1/policy` | — | xem chính sách đang hoạt động |
| `GET /v1/policy/versions` | — | liệt kê phiên bản đã lưu + đang hoạt động |
| `POST /v1/policy/validate` | bearer | kiểm tra biên dịch một chính sách mà không áp dụng |
| `POST /v1/policy` | bearer | lưu + kích hoạt phiên bản mới |
| `POST /v1/policy/versions/{v}/activate` | bearer | kích hoạt phiên bản đã lưu (hồi lùi) |
| `GET /healthz` | — | liveness |
| `GET /readyz` | — | sẵn sàng (tường lửa hoàn toàn up) |
| `GET /metrics` | — | metrics Prometheus |

- **Auth:** đặt `api_token` (cấu hình) hoặc `GOSHISEN_API_TOKEN`. Nó không bao giờ được chấp nhận như cờ CLI. Khi không có token, thao tác quản trị bị từ chối (401).
- **Đồng thời:** áp dụng chính sách yêu cầu `base_version` bằng phiên bản đang hoạt động; ngược lại `409 version_conflict`. Do đó các writer đồng thời không thể âm thầm ghi đè lẫn nhau.
- **Chính sách không hợp lệ:** bị từ chối với `422 invalid_policy` trước mọi thay đổi trạng thái; chính sách đang hoạt động được giữ nguyên.
- **Bền vững:** chính sách được lưu thành các tệp phiên bản bất biến với con trỏ active cập nhật nguyên tử; phục hồi khi khởi động lại lại nạp phiên bản đang hoạt động. `policy_dir` cấu hình kho. `SIGHUP` nạp lại tệp chính sách đã cấu hình qua cùng pipeline bền vững như API (mỗi lần nạp lại thành một phiên bản kho mới), nên chính sách của daemon và phiên bản active của kho không bao giờ lệch nhau.
- **Công bố nguyên tử:** chính sách chỉ được chuyển sau khi được xác thực đầy đủ và kho đã bền vững; khi có lỗi (YAML không hợp lệ, biên dịch lỗi, ghi kho lỗi), chính sách trước vẫn hoạt động trong cả kho lẫn ruleset sống. `goshisen_policy_updates_total{outcome="success"|"failure"|"conflict"}` ghi nhận từng lần thử.
- **Kiểm toán:** mọi hành động quản trị được ghi log (hành động, kết quả, phiên bản); token không bao giờ được ghi log.

Ví dụ (cấu hình mẫu không có token; đặt qua env cho ví dụ):

```bash
export GOSHISEN_API_TOKEN="set-a-real-token"
# Kiểm tra trước:
curl -s -X POST -H "Authorization: Bearer $GOSHISEN_API_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"policy\": \"$(cat policies/examples/allow-lab.yaml | tr '\n' ' ')\"}" \
  http://127.0.0.1:8080/v1/policy/validate
# Áp dụng (phiên bản 1):
curl -s -X POST -H "Authorization: Bearer $GOSHISEN_API_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"policy\": \"$(cat policies/examples/allow-lab.yaml | tr '\n' ' ')\", \"base_version\": \"\"}" \
  http://127.0.0.1:8080/v1/policy
```

## Metrics

Metrics văn bản Prometheus tại `GET /metrics` (API) và trên `metrics_port` (mặc định 9090): verdict gói theo hành động/giao thức/trạng thái, lỗi backend, histogram độ trễ verdict, phiên bản chính sách đang hoạt động. Label là enum giới hạn — không có IP, cổng, hoặc flow ID.

## Nhận thức flow-offload

Flow offloading nftables (phần mềm hoặc phần cứng) khiến luồng established bỏ qua toàn bộ ruleset nftables, nên NFQUEUE không bao giờ thấy chúng — thực thi không áp dụng cho các luồng đó. Daemon phát hiện điều này chỉ-đọc (flowtables + câu lệnh `flow add @ft` / `flow offload`) mỗi 10 s, ghi log `WARN` mức nghiêm trọng cao, và lộ gauge `goshisen_flow_offload_active`. Nó không bao giờ sửa tường lửa của người dùng.

`offload_policy` (file hoặc `GOSHISEN_OFFLOAD_POLICY`; mặc định `warn`) chọn cách daemon phản hồi:

| Giá trị | Hành vi |
| --- | --- |
| `warn` (mặc định) | ghi WARN và lộ gauge; sẵn sàng không đổi |
| `refuse` | lỗi khởi động **trước** khi cài hook khi phát hiện offloading (fail-closed bằng vắng mặt: hệ thống không bị động đến). Thoát 1 với lỗi có thể hành động |
| `readiness` | tiếp tục thực thi, nhưng không bao giờ sẵn sàng trong khi offloading hoạt động (`GET /readyz` trả 503); watcher 10 s hạ sẵn sàng nếu offloading xuất hiện sau đó và khôi phục khi ruleset sạch trở lại |

## Giám sát tràn hàng đợi

Khi hàng đợi NFQUEUE của kernel đầy (hoặc socket netlink của daemon không thoát được), kernel DROP các gói dư thừa — **nó không bao giờ chấp nhận chúng**: daemon không bao giờ đặt cờ fail-open của kernel. Do đó gói bị DROP không bao giờ vòng qua tường lửa.

Daemon hỏi `/proc/net/netfilter/nfnetlink_queue` (hàng đợi nằm trong namespace mạng của daemon) mỗi giây một lần và:

- ghi nhận `goshisen_queue_overflow_total{cause="kernel_drop"}` (DROP do hàng đầy) và `{cause="user_drop"}` (lỗi unicast netlink);
- ghi dòng `WARN` kèm bộ đếm DROP (giới hạn tần suất một lần mỗi 10 s).

Thống kê hàng đợi chỉ có khi kernel lộ tệp proc; ngược lại giám sát bị tắt im lặng. `queue_maxlen` cấu hình độ sâu hàng đợi (nhỏ hơn thì tràn sớm hơn — hữu ích cho kiểm thử/xác thực).

## Tệp chính sách

Lược đồ và ngữ nghĩa chính sách YAML được quy định trong `docs/design/architecture.md` §4–§5. Bộ so khớp hiện tại:

- `id` (bắt buộc, duy nhất), `action` (`ACCEPT`/`DROP`), `protocol` (`tcp`/`udp`/`any`), `src`/`dst` (IPv4 CIDR, địa chỉ trần, hoặc `any`), `src_port`/`dst_port` (một cổng hoặc `lo:hi` bao gồm, chỉ khi có giao thức tường minh), `state` (`NEW`, `ESTABLISHED`, `RELATED`, `INVALID`), `log`, `description`.
- Quy tắc có thứ tự; cái khớp đầu tiên quyết định. Nếu không có gì khớp, `default_action` được áp dụng (mặc định `DROP`).
- **Ngữ nghĩa có trạng thái:** siêu dữ liệu conntrack của kernel quyết định trạng thái và hướng của gói. Gói `NEW` là sự kiện chấp nhận; `ESTABLISHED`/`RELATED` chỉ khớp các kết nối mà chính sách đã chấp nhận (một tập accepted-tracking ghi nhận chúng). Lưu lượng trả lời/mang lậu cho kết nối chưa được chấp nhận bị DROP kể cả khi tuple khớp.
- Trường không biết, ID trùng, CIDR/cổng sai định dạng, trạng thái không hợp lệ, và bộ so khớp cổng không có giao thức là lỗi biên dịch. `in_iface`/`out_iface` vẫn dành riêng và bị từ chối.
- Kiểm tra chính sách mà không áp dụng: `goshisen policy validate --policy-file policies/examples/allow-lab.yaml`.
- Mô phỏng có trạng thái: `goshisen policy simulate --state established ...` (mặc định: không trạng thái).

## Ví dụ

```bash
# Kiểm tra mà không chạy:
goshisen -c configs/goshisen.yaml config check

# Chỉ dùng cờ (không tệp cấu hình), để mức log mặc định:
goshisen --interface eth0 --policy-file policies/default/deny-all.yaml

# Kết hợp môi trường + tệp:
GOSHISEN_LOG_LEVEL=debug GOSHISEN_LOG_JSON=true \
  goshisen -c configs/goshisen.yaml

# Xem cấu hình hiệu lực và nguồn:
goshisen -c configs/goshisen.yaml config show

# Kiểm tra một chính sách và xem các quy tắc đã biên dịch:
goshisen policy validate --policy-file policies/examples/allow-lab.yaml

# Xem trước việc công bố daemon sẽ thực hiện (đọc kho, không ghi gì):
goshisen policy dry-run --policy-file policies/examples/allow-lab.yaml \
  --policy-dir policy_store
#   policy .../allow-lab.yaml: valid, N rule(s), default action DROP
#   dry-run: would publish version 2 (active: 1)
#   rules: 1 -> 2 (+1)
#   default action: DROP -> DROP
#   target: .../policy_store/2.yaml (version 2) — nothing written

# Mô phỏng gói TCP tới cổng 80 (NEW, nên quy tắc allow-http khớp):
goshisen policy simulate --policy-file policies/examples/allow-lab.yaml \
  --protocol tcp --state new --src 10.0.0.5 --dst 10.0.0.2 --dest-port 80
#   verdict: ACCEPT
#   matched rule: allow-http

# Mô phỏng gói không quy tắc nào khớp (hành động mặc định):
goshisen policy simulate --policy-file policies/examples/allow-lab.yaml \
  --protocol udp --state new --src 10.0.0.5 --dst 10.0.0.2 --dest-port 9999
#   verdict: DROP
#   matched rule: (default)
```
