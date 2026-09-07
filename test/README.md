# Stage-4 end-to-end test

`TestStage4EndToEnd` proves group fan-out + echo-to-sender, stanza dedup
replay, history authz, mute enforcement, signaling bypass with zero DB
pollution, and kick revocation on the fully-wired app.

```sh
go test ./test/ -run TestStage4EndToEnd -v
```
