# imcli cheat sheet (Stage-1 verification client)

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
