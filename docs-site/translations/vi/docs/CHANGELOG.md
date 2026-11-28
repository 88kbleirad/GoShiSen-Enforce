# Nhật ký thay đổi (Changelog)

Trạng thái và lịch sử đáng chú ý của GoShiSen-Enforce. Chi tiết theo từng bản phát hành nằm trong `docs/release/` (vd: `v0.2.6-notes.md`, `proposal-next-version-scope.md`).

## Trạng thái phát hành (ảnh chụp, 2026-11-27)

- **v0.2.9** — bản phát hành mới nhất (đã công bố 2026-11-27): bản phát hành về kỹ thuật phát hành / làm cứng CI với **không thay đổi runtime** (CLI hoạt động y hệt v0.2.8): cổng phê duyệt F-02 cho job `sign`, harness e2e OpenWrt được làm cứng, các bước mạng trong CI có giới hạn, gate S-005 govulncheck fail-closed, công thức build apk được đưa vào repo kèm gate build manifest, và PR pin sau tag + neo recipe ở bước 9. Ghi chú: `docs/release/v0.2.9-notes.md`; asset: [Releases](https://github.com/88kbleirad/GoShiSen-Enforce/releases/tag/v0.2.9).
- **v0.2.8** — bản phát hành trước (đã công bố 2026-11-22): bản phát hành bảo mật/làm cứng — đọc file có giới hạn ở mọi chỗ đọc đầu vào của người vận hành, policy store riêng tư (0700/0600) với listener metrics có giới hạn thời gian, UCI `api_token` được ghi 0600, các tùy chọn yêu cầu xác thực khi đọc (D1), cân bằng lại govulncheck (D2). Ghi chú: `docs/release/v0.2.8-notes.md`; asset: [Releases](https://github.com/88kbleirad/GoShiSen-Enforce/releases/tag/v0.2.8).

## Xoay anchor ký phát hành (2026-11-28, sau v0.2.9)

Anchor tin cậy của bản phát hành CLI đã được xoay: `keys/goshisen-release.pub.pem`
nay là khóa Ed25519 có vân tay
`e9b4bf1194ce90d2377720102748460404eacbc7aba9aa8ad48532cd63294fb9`
(key ID `bdda690b97a4f49e`). Anchor trước khi xoay
(`24215e016621925215dec0ad6ea14ac74253a4e97249a973e89d649a2217cca0`, tạo
2026-11-13) đã ký **v0.2.1 – v0.2.9** và vẫn được commit tại
`keys/goshisen-release-2026-11-13.pub.pem`.

- **Khi xác minh bản cũ:** dùng asset `public_key.pem` đi kèm chính bản đó (hoặc
  anchor lưu trữ ở trên). `main` nay mang anchor đã xoay và sẽ **không** xác minh
  được chữ ký v0.2.x — tài liệu cài đặt đã được cập nhật tương ứng.
- Không thay đổi hành vi runtime/CLI; không có artifact đã phát hành nào bị ký
  lại hay thu hồi.
- Bản ghi: [`docs/release/release-key-rotation-2026-11-28.md`](release/release-key-rotation-2026-11-28.md).

## v0.2.9 (đã công bố 2026-11-27)

Bản phát hành về kỹ thuật phát hành và làm cứng CI. **Không có thay đổi mã daemon
kể từ v0.2.8** (`git diff 'v0.2.8^{}'..'v0.2.9^{}' -- '*.go' internal cmd go.mod
go.sum` rỗng): CLI không đổi và payload apk chỉ khác ở dấu phiên bản nhúng trong
binary. Chi tiết: `docs/release/v0.2.9-notes.md`.

- **F-02 (#132):** job `sign` trong `release.yml` khai báo `environment: signing`
  (có required reviewer, policy `main`/`v*`), nên một bản phát hành theo tag sẽ
  dừng chờ phê duyệt trước khi đọc `GH_RELEASE_SIGNING_KEY`; guard rule mới
  `environment-scoped` cùng fixture âm `bad-sign-no-environment.yml` làm CI fail
  nếu một job `sign` mất cổng; environment `release-cli` thừa đã bị xóa. Bản
  thân **key CLI vẫn ở repo scope** — cổng bảo vệ job, không bảo vệ key
  (`gh secret list --env signing` chỉ có `GH_RELEASE_APK_SIGNING_KEY`); các bước
  còn lại trong `docs/release/f02-environment-migration.md` §0/§3.
- **Harness e2e OpenWrt (#134):** lab khởi động daemon tường minh khi `rc.local`
  thua cuộc đua lúc boot (kiểm tra readiness theo proc trước, fallback `setsid`,
  thăm dò lại 120 giây) và thu thập chẩn đoán *trước* `lab down`.
- **Các bước mạng trong CI có giới hạn (#135):** `CURL_BOUNDED`/`APT_BOUNDED` +
  timeout cho từng job; đo được: `sign + verify` 37m16s → 46–58 giây,
  `integration` hơn 50 phút → khoảng 3 phút.
- **Gate S-005 govulncheck fail-closed (#137):** `-format json` trả mã 0 dù có
  phát hiện, nên gate cũ có thể cho qua một lượt quét hỏng hoặc có lỗ hổng
  (stream rỗng bị coi là sạch, `${arr[@]:-}` khớp với một mục allowlist rỗng).
  Hai gate nay dùng chung `scripts/lib/govuln-verdict.sh` và fail khi mã trả về
  bất thường, stream không phân tích được/rỗng, phát hiện không có `osv`, hoặc
  phát hiện không nằm trong allowlist; thêm job CI chạy fixture cho CLI archive.
  Các artifact đã công bố v0.2.6–v0.2.8 được quét lại thô bằng
  `govulncheck -mode=binary` (9 artifact, 0 bản ghi finding) — **không có gì bị
  che giấu**.
- **Công thức build apk được đưa vào repo + gate manifest (#146), pin sau tag
  (#148), neo recipe ở bước 9 (#149):** `scripts/build-apk-release.sh` build apk
  chưa ký chỉ từ đầu vào đã commit (SDK ghim theo digest đọc từ `apk-sign.yml`,
  go1.27.4 ghim sha256, mirror hash dẫn xuất bằng hai lượt zero-hash) và ghi
  `build-manifest-<label>.json`; `scripts/check-apk-build-manifest.sh` dẫn xuất
  lại manifest, bước 9 của `release-pin-gate.sh` neo recipe vào tag (cách nối cũ
  từ chối luồng pin-commit đã tài liệu hóa; fixture nay bao phủ `F != T` cùng
  hai ca âm về trôi recipe sau tag).
- **Artifact (20):** apk `goshisen-0.2.9-r1.apk`
  `1604a6802de6784b769075a98f437b2a6ea8375e1266f79221acd48d3102dfd7` được ký bằng
  anchor EC P-256 (fingerprint `542bb7ae…`; payload `ba8183b1…`, go1.27.4); CLI
  `b6c5b8cd…` (amd64) / `50c0892e…` (arm64) / sbom `c25f868a…`. Apk cố ý không
  nằm trong `checksums.txt` (miền khóa riêng) — `scripts/verify-release-assets.sh`
  kiểm toàn bộ bộ asset và fail-closed với apk chưa ký.
- **Kiểm chứng trên VM:** cài apk đã ký không dùng `--allow-untrusted`, queue
  100/101, cặp kiểm soát DROP/ACCEPT và dọn dẹp `apk del` đều PASS; nâng cấp
  v0.2.8 → v0.2.9 dưới thăm dò liên tục cho thấy **khoảng trống fail-closed ~0,4
  giây** lúc restart (hook vẫn được giữ trong khi queue gắn lại — kết nối mới bị
  drop, không có gói nào được chấp nhận trái policy).
- **Giới hạn đã biết:** tag phát hành là annotated nhưng chưa ký; `main` và các
  tag không có bảo vệ (ruleset rỗng); PR #150 (readiness cho test NAT) đang mở và
  chỉ sửa test.

## v0.2.8 (đã công bố 2026-11-22)

- **Siết quyền thư mục policy store + giới hạn listener metrics (#102):** policy store tạo thư mục với quyền `0700` và ghi file policy/`active.json` với quyền `0600` (trước là `0755`/`0644`), nên tài khoản cục bộ khác không đọc được policy đang áp dụng; listener HTTP metrics nay đặt `ReadTimeout`/`WriteTimeout` 10 giây và `IdleTimeout` 30 giây, khớp với listener API, nên client cục bộ bị treo không thể giữ kết nối (và goroutine) mãi. Test hồi quy: `TestOpenRestrictedPerms`.
- **Đọc file có giới hạn (#104):** đọc config/policy/store giờ từ chối file không-thường trước khi mở và giới hạn 16 MiB — đường dẫn policy trỏ vào /dev/urandom trước đây treo vô hạn (bộ nhớ ~1 GiB/s).
- **CI mạnh hơn (#105–#107):** sweep bất thường CLI 257 case và các mục tiêu fuzz config đã được đưa vào repo và chạy trong CI, cùng staticcheck (baseline rỗng) và govulncheck source mode.
- **UCI api_token (#108, đóng #103):** cấu hình OpenWrt giờ đặt được token API; config daemon được render có chmod 600 và charset token bị giới hạn (A-Za-z0-9._-); init script cảnh báo khi listener API bật mà không có token.
- **Đọc thống kê queue cũng có giới hạn (mở rộng P1.1 của v0.2.8):** `packet.ReadQueueStats` (đọc `/proc/net/netfilter/nfnetlink_queue`, dùng bởi watcher cảnh báo overflow) là chỗ `os.ReadFile` không giới hạn cuối cùng trong cây mã; nay đi qua `internal/safefile.ReadBounded`, nên file không-thường (FIFO, device, thư mục) hoặc dữ liệu vượt hạn sẽ fail-closed thay vì treo daemon. Test hồi quy bao phủ: file thường, file thiếu, FIFO (không được block), thư mục và kích thước vượt hạn.
- **Kiểm tra đường dẫn policy lúc khởi động cũng có giới hạn (F-4a/F-4b):** bước tiền-kiểm tra của daemon mở `policy_file` bằng `os.Open` thường, nên một đường dẫn tới FIFO sẽ chặn khởi động vô hạn, còn thư mục/thiết bị vẫn được chấp nhận và chỉ lỗi sau đó. Nay bước kiểm tra đi qua bộ đọc có giới hạn (mở non-blocking, kiểm tra file thường, giới hạn 16 MiB), nên đường dẫn không dùng được sẽ fail nhanh với lỗi rõ ràng `policy_file:`. Test bao phủ: FIFO (không được block), thư mục, thiết bị, vượt hạn và file thường.

## Chi tiết đã chuyển ra khỏi README

- Bộ tích hợp: 21 kịch bản netns (e2e, state, NAT, crash, overflow trong `tests/integration/`), gated bằng root, chạy với `sudo make integration`; CI chạy toàn bộ bộ trên runner GitHub (21/21 PASS, không có skip bắt buộc).
- Xác thực OpenWrt: OpenWrt 25.12.6 x86/64 (kernel 6.12.112); dịch vụ tắt mặc định; IPv6 stateful ACCEPT đã xác minh trên kernel 6.12.112 với mẫu quy tắc `ESTABLISHED/RELATED` hai chiều chuẩn — `docs/openwrt.md`, `docs/release/issue-5-investigation.md`.
- Đường nóng không cấp phát (0 allocs/gói); benchmark: `docs/design/performance.md`.
- Danh mục metric (12 họ) và thiết lập Grafana: `docs/release/metrics-grafana-validation.md`.
- Cấu trúc kho: `docs/developer-guide.md`.

## Các bản sửa kiểm toán (phát hành trong v0.2.6, 2026-11-18)

- **Phân tích cổng nghiêm ngặt (F-001):** các bộ so khớp `src_port`/`dst_port` trong chính sách và giá trị env `GOSHISEN_API_PORT`/`GOSHISEN_METRICS_PORT` giờ phải là số nguyên thuần. Trước đây `fmt.Sscanf("%d")` âm thầm chấp nhận ký tự rác theo sau (`"80evil"` → 80), nên một lỗi gõ có thể thay đổi chính sách được thi hành hoặc cổng ràng buộc thay vì làm hỏng cấu hình.
- **Gán lỗi backend (F-002):** lỗi socket NFQUEUE phía nhận giờ được báo cáo là `kind=receive` (khớp với bộ nhãn `goshisen_backend_errors_total` đã ghi lại) thay vì `kind=send`.

## Các bản sửa bảo mật (phát hành trong v0.2.6, 2026-11-19)

- **Bảo vệ chèn tên thẻ phát hành (S-003):** workflow phát hành giờ xác thực tên thẻ được đẩy lên theo `^v[0-9]+\.[0-9]+\.[0-9]+$` trong bước đầu tiên trước mọi thứ khác, truyền qua biến môi trường và đặt trong dấu nháy tại mọi nơi sử dụng; chế độ production của `apk-sign.yml` xác thực ref của nó cùng cách. Job CI `release-tag-guard` chứng minh các fixture tên thẻ chứa ký tự đặc biệt shell bị từ chối trước khi dùng. Không kèm bước tái tạo công khai cho đến khi bản này hạ cánh.
- **Ảnh container ghim theo digest (S-004):** `openwrt/sdk` (`apk-sign.yml`), `golang:1.25` và `gcr.io/distroless/base-debian13` (Dockerfile) được ghim theo digest; ghi chú cập nhật tại mỗi lần ghim.
- **Hành động CI trên runtime Node 24:** actions/checkout v7.0.1, actions/setup-go v7.1.0, actions/upload-artifact v7.0.2 (ghim SHA), loại bỏ cảnh báo hết hạn Node.js 20 trên mọi job.
- **Dockerfile và CI trên go1.27.4 (S-001):** ảnh nền Dockerfile được nâng từ golang:1.25 (go1.25.14, 11 lỗ hổng stdlib có thể tiếp cận) lên `golang:1.27.4@sha256:205c7636...`; mọi job CI cài go1.27.4.
- **Cổng payload apk (S-005):** payload apk v0.2.5 đã công bố được build với go1.27.1 → 11 lỗ hổng stdlib có thể tiếp cận (issue #78). Đã đóng trong v0.2.6 (payload apk + cả hai kho lưu trữ CLI build với go1.27.4). Hai cổng chặn fail-closed trên đường ký giờ chạy trong chế độ production của `apk-sign.yml` trước khi ký, cùng các job hồi quy CI:
  - `scripts/check-apk-payload-go.sh` từ chối mọi apk có payload được build với Go < `MIN_GO_VERSION` (`packaging/go-toolchain.env`, 1.27.4);
  - `scripts/check-apk-payload-govuln.sh` chạy govulncheck chế độ binary trên payload đã giải nén và thất bại nếu có lỗ hổng stdlib/module có thể tiếp cận không nằm trong `packaging/govulncheck-allowlist.txt` (trống theo mặc định). Payload phải mang buildinfo Go đọc được, nên công thức gói v0.2.6 bỏ `-buildid=` và đặt `STRIP:=true` — `sstrip` mặc định của SDK gỡ bảng chuỗi mục ELF, làm hỏng `go version -m` và quét chế độ binary ngay cả khi giữ `-buildid=` (công thức v0.2.5 bỏ cả hai, làm hỏng hoàn toàn việc quét chế độ binary).
  Feed được nướng sẵn của ảnh SDK hiện tại CHƯA build Go 1.27.4 (mặc định feed go1.26.9, golang1.27 go1.27.1; ghim lại feed về commit mới hơn làm hỏng quét metadata của SDK này) — bản build v0.2.6 gắn toolchain go1.27.4 được xác minh sha256 (`scripts/sdk-build-v026.sh`, phương án 3 trong báo cáo S-005). Apk v0.2.5 đã phân phối không thay đổi.
- **Toolchain ghim go1.27.4 (S-001):** 11 lỗ hổng thư viện chuẩn có thể tiếp cận (net/http Range-header DoS, chạy đua MaxBytesReader, chạy đua HPACK http2, crypto/x509) dưới go1.27.1 đã đóng; `golang.org/x/net` v0.62.0 và `golang.org/x/sys` v0.49.0. Chỉ thị `go` giờ là `go 1.26.0` (yêu cầu Go ≥ 1.26; CI cài trực tiếp go1.27.4).
- **queue_num giới hạn 0..65534 (S-002):** queue_num 65535 từng được chấp nhận nhưng không bao giờ cài được (quy tắc queue IPv6 tại queue_num+1 sẽ thành `queue to 65536`, ngoài phạm vi u16); cấu hình giờ từ chối nó kèm thông báo rõ ràng.

## Hiệu chỉnh (tài liệu hậu v0.2.6, 2026-11-19; tái xác minh 2026-11-20)

- **Mặc định listener HTTP (hiệu chỉnh tài liệu):** ghi chú v0.2.6,
  cảnh báo bảo mật v0.2.5 và tài liệu OpenWrt từng nói các listener
  HTTP "bị tắt theo mặc định". Thực tế mặc định tích hợp của daemon là
  API `127.0.0.1:8080` và metrics `127.0.0.1:9090` (loopback) sau khi
  dịch vụ được bật; cấu hình OpenWrt được phân phối lưu
  `api_port '0'`/`metrics_port '0'`, nhưng v0.2.6 bỏ qua giá trị `0`
  từ tệp/cờ (quay về mặc định; `internal/config/config.go` và
  `internal/cli/cli.go`). Cách duy nhất để tắt listener hiện nay là
  `GOSHISEN_API_PORT=0` / `GOSHISEN_METRICS_PORT=0`, hoặc giữ dịch vụ
  tắt (`enabled=0`). Không có thay đổi hành vi mã; mức độ nghiêm trọng
  vẫn là Low (mặc định) / Medium (`api_host` không-loopback).
  Bản vá mã để tôn trọng `0` tường minh dự kiến ở v0.2.7.
- **Mặc định listener đo được (2026-11-20), có cả kho lưu trữ CLI:**
  cùng hiệu chỉnh áp dụng cho **kho lưu trữ CLI** v0.2.5/v0.2.6,
  trước đây chỉ được mô tả là "chỉ loopback". Đo được khi không có
  cấu hình/env/cờ, chúng lắng nghe trên `127.0.0.1:8080` +
  `127.0.0.1:9090`; YAML `api_port: 0`, giá trị YAML trống và
  `--api-port 0` bị bỏ qua ở đó, trong khi `GOSHISEN_API_PORT=0` tắt.
  OpenWrt: cài đặt mới gói apk 0.2.5-r1 / 0.2.6-r1 đã ký với cấu
  hình đi kèm, và nâng cấp tại chỗ 0.2.3-r1 → 0.2.6-r1 trong khi đang
  chạy, cả hai đều kết thúc với hai listener loopback hoạt động; cấu
  hình không có các tùy chọn (hoặc để trống) hủy bỏ trong script init
  (`api_port '' out of range`) mà không có listener. Thứ tự ưu tiên
  được xác nhận bằng `goshisen config show`: mặc định < tệp < env <
  cờ. Bảng bằng chứng: `docs/security/advisory-v0.2.5-go-toolchain.md`
  § "Measured listener defaults"; kiểm thử hợp đồng:
  `tests/package/listener_defaults_contract_test.go`. Bản vá mã tôn
  trọng `0` tường minh có trên `main` sau thẻ v0.2.6 nhưng **chưa có
  trong bất kỳ artifact nào đã công bố** (ứng viên v0.2.7).

## Chưa phát hành (sau v0.2.6)

- **Giá trị `0` tường minh tắt listener HTTP (khắc phục lỗ hổng tài
  liệu v0.2.6):** `api_port: 0` / `metrics_port: 0` trong tệp cấu hình
  và `--api-port 0` / `--metrics-port 0` trên dòng lệnh giờ tắt
  listener API / metrics thay vì bị coi là "chưa đặt" và quay về
  8080/9090. Lớp tệp theo dõi sự hiện diện của khóa
  (`File.UnmarshalYAML`); lớp cờ dùng `flag.Value` ghi nhận sự hiện
  diện (`portFlag`, cũng từ chối giá trị ngoài phạm vi tại thời điểm
  phân tích). Cổng chưa đặt vẫn giải quyết về mặc định loopback tích
  hợp. Cấu hình OpenWrt được phân phối (`option api_port '0'`) giờ tắt
  cả hai listener như mong muốn; `GOSHISEN_API_PORT=0` /
  `GOSHISEN_METRICS_PORT=0` hoạt động như trước. Kiểm thử:
  `TestLoadFileZeroPortsDisable`, `TestLoadFilePortPresence`,
  `TestLoadFlagZeroPortDisables`, `TestLoadFilePortRejectsNonScalar`,
  `TestParsePortZeroFlags`, `TestParsePortInvalidValues`. Không thay
  đổi mức độ nghiêm trọng bảo mật (mặc định loopback-only).
- **Fixture lab bật listener metrics tường minh:** ảnh lab QEMU
  (`scripts/openwrt-image-prep.sh`) là người dùng duy nhất cần listener —
  nó đọc bộ đếm phán quyết từ `127.0.0.1:9090/metrics` — nên giờ nó
  ghi `metrics_port: $LAB_METRICS_PORT` (9090) thay vì dựa vào `0`
  quay về mặc định; `api_port` vẫn là `0` (chính sách được áp dụng qua
  SIGHUP). Cả hai script e2e đọc `${LAB_METRICS_PORT:-9090}` và thất
  bại nhanh với `ENV: metrics listener unreachable` khi snapshot không
  lấy được metrics, thay vì thông báo gây hiểu lầm "no daemon DROP
  verdicts recorded". Kiểm thử: `TestProvisionLabMetricsPort`.

## Chưa phát hành (sau v0.2.8)

- **Hai cổng S-005 govulncheck đã "fail open" khi bản quét hỏng (bản sửa bảo mật):**
  cả hai cổng trên đường ký — `scripts/check-apk-payload-govuln.sh` và
  `scripts/check-cli-archive-govuln.sh` — mang bản sao của cùng logic phán quyết,
  và bản sao đó có thể in `S-005-…-GOVULN-GATE-PASS` cho một bản quét đã phát
  hiện lỗ hổng hoặc chưa hề hoàn tất. Đo trên các script trước khi sửa với
  `govulncheck` giả lập: exit 3 với luồng rỗng, exit 3 với một mục finding không
  mang OSV id, và exit 137 (SIGKILL) đều PASS, tất cả đều được báo là "findings
  are allowlisted", vì phép so khớp allowlist chạy `"${findings[@]:-}"` với
  `"${allowed[@]:-}"` — trên mảng RỖNG, biểu thức này mở rộng thành một từ rỗng,
  nên `[ "" = "" ]` khớp. Cả hai cổng nay dùng chung một bản cài đặt,
  `scripts/lib/govuln-verdict.sh`, phán quyết dựa trên luồng đã phân tích chứ
  không dựa vào rc: luồng không phân tích được hoặc rỗng, mục finding không có
  OSV id, finding không nằm trong `packaging/govulncheck-allowlist.txt`,
  `govulncheck` thiếu hoặc không thực thi được, và mọi mã thoát khác 0/3 đều làm
  cổng thất bại; findings được in dù cổng đạt hay không đạt. Mã dùng chung cũng
  bỏ giả định rằng `-mode=binary -format json` thoát với mã 3 khi phát hiện lỗ
  hổng — govulncheck ghi rõ là thoát 0 trong trường hợp đó
  (`cmd/govulncheck/doc.go`, v1.9.0) và thực tế thoát 0, nên findings đến với
  rc 0. Bao phủ hồi quy: ma trận kết quả với tool giả lập trong
  `tests/ci/check-apk-payload-govuln-test.sh` (rc 0/3 × luồng sạch / không nằm
  allowlist / nằm allowlist / không có OSV id / rỗng / không phân tích được, cùng
  tool thiếu và tool không thực thi được) và các fixture dùng tool thật trong cả
  hai test cổng; fixture 1 ở cả hai test nay còn khẳng định hình dạng luồng của
  đối chứng sạch — có ít nhất một bản ghi `osv` và không có bản ghi `finding`
  nào, qua `tests/ci/lib/govuln-clean-stream.sh` — nên đối chứng đạt vì đúng lý
  do, và các fixture của cổng CLI chạy trong CI với tên job
  `cli-archive-govuln-gate (S-005)` (trước đây không chạy trong CI). Không
  artifact nào đã phát hành thay đổi — chỉ cổng và CI.
- **Recipe dựng apk đã được commit và artifact được gắn với nó bằng manifest
  (#146):** các apk v0.2.6–v0.2.8 được dựng từ một bản sao recipe *cục bộ, chưa
  commit* (tắt phụ thuộc `golang/host` của SDK và mount toolchain go1.27.4 chính
  thức), nên không ai khác tái lập được.
  `packaging/apk-build/no-golang-host.patch` là thay đổi recipe duy nhất đó dưới
  dạng patch đã commit, `packaging/apk-build/sdk-x86_64.config` là mảnh config
  SDK dùng cho apk v0.2.8 đã công bố, và `packaging/go-toolchain.env` ghim
  `GO_TOOLCHAIN_*` (sha256 của tarball go1.27.4 và `bin/go`).
  `scripts/build-apk-release.sh` dựng gói chưa ký chỉ từ đầu vào đã commit: nó
  `git archive` theo `--commit`, áp overlay lên bản sao scratch (không bao giờ
  lên `openwrt/Makefile` trong cây làm việc), tự suy ra hoặc kiểm tra
  `PKG_MIRROR_HASH`, dùng digest SDK ghim trong `apk-sign.yml`, kiểm tra hash
  tarball/`bin/go` của Go, khẳng định gói chưa ký ngay trong container
  (`apk verify` → `UNTRUSTED signature`), và ghi `build-manifest-<label>.json`.
  `scripts/check-apk-build-manifest.sh` tái suy ra mọi khẳng định trong manifest
  từ commit — hash recipe, overlay và config SDK, replay recipe tới hash Makefile
  đã ghi, quan hệ tổ tiên của pin, ghim Go và SDK, hash artifact, trạng thái
  chưa ký, và (với `--compare`) sự đồng thuận A/B.
  `scripts/release-pin-gate.sh` có thêm bước 9 tùy chọn (`--manifest` /
  `GATE_MANIFEST`) để một bản phát hành có thể bắt buộc bước này, và CI chạy
  `tests/ci/apk-build-manifest-test.sh` (fixture sinh tự động; mọi ca âm đều bị
  từ chối với lý do được nêu tên) với tên job `apk-build-manifest-gate (F-01)`.
  Đã xác minh trước khi merge: hai bản dựng độc lập trên cùng một commit cho
  hash apk (`fa3b9530...`, 3.286.016 B) và payload (`ba8183b1...`) giống nhau,
  và chính script đó dựng lại đầu vào v0.2.8 tái tạo apk trước khi ký đã công bố
  `b97f0da4...` giống hệt từng byte. Đây không phải bản dựng SDK nguyên sạch
  (toolchain Go nằm ngoài cây SDK) và mirror hash phụ thuộc toolchain —
  `docs/release/v0.2.9-apk-build-recipe.md` ghi lại các lần chạy và những giới
  hạn đó. Không thay đổi mã runtime; chỉ đóng gói, CI và tài liệu.
