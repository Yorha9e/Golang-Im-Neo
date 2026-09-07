# imcli cheat sheet (Stage-1+2+4 verification client)

```sh
go run ./cmd/client -action register -user alice -pass password123
go run ./cmd/client -action login -user alice -pass password123
go run ./cmd/client -action ticket -user alice
go run ./cmd/client -action chat -user alice -msg "hi all" -listen 5s
go run ./cmd/client -action chat -user alice -to <bob_uid> -msg "hi" -listen 10s
go run ./cmd/client -action dedup-test -user alice
go run ./cmd/client -action e2e
```

Session caches to `.imcli-<user>.json`; server via `-server URL`.

## Stage-4 group + signaling

```sh
go run ./cmd/client -action group-create -user alice -msg "stage4"
go run ./cmd/client -action group-list -user alice
go run ./cmd/client -action group-invite -user alice -group <gid> -target <bob_uid>,<carol_uid>
go run ./cmd/client -action group-members -user alice -group <gid>
go run ./cmd/client -action group-chat -user alice -group <gid> -msg "hello group" -listen 5s
go run ./cmd/client -action group-history -user bob -group <gid> -limit 20
go run ./cmd/client -action group-mute -user alice -group <gid> -target <carol_uid>
go run ./cmd/client -action group-unmute -user alice -group <gid> -target <carol_uid>
go run ./cmd/client -action group-role -user alice -group <gid> -target <bob_uid> -action2 admin
go run ./cmd/client -action group-kick -user alice -group <gid> -target <carol_uid>
go run ./cmd/client -action signal-send -user alice -to <bob_uid> -action2 offer -msg "sdp-offer-x" -listen 5s
go run ./cmd/client -action signal-listen -user bob -listen 30s
```

Example session: alice creates a group, invites bob (carol joins herself),
alice sends a group-chat frame (ACK + echo + fan-out), and alice/bob can
exchange WebRTC signaling once they are mutual friends:

```sh
go run ./cmd/client -action register -user alice -pass password123
go run ./cmd/client -action register -user bob -pass password123
go run ./cmd/client -action login -user alice -pass password123
go run ./cmd/client -action login -user bob -pass password123
go run ./cmd/client -action group-create -user alice -msg "stage4"   # prints group_id=<gid>
go run ./cmd/client -action group-invite -user alice -group <gid> -target <bob_uid>
go run ./cmd/client -action group-chat -user alice -group <gid> -msg "hello group" -listen 5s
go run ./cmd/client -action group-history -user bob -group <gid> -limit 20
go run ./cmd/client -action friend-apply -user alice -target bob
go run ./cmd/client -action friend-respond -user bob -target <alice_uid> -action2 accept
go run ./cmd/client -action signal-send -user alice -to <bob_uid> -action2 offer -msg "sdp-offer-x" -listen 5s
```
