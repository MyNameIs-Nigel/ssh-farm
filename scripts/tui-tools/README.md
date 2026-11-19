# SSH terminal capture

From the repository root, start an isolated development instance in one shell. A fresh temporary directory gives each run its own database and host key.

```sh
dev_dir=$(mktemp -d)
FARM_LISTEN_HOST=127.0.0.1 \
FARM_LISTEN_PORT=2223 \
FARM_DB_PATH="$dev_dir/game.db" \
FARM_HOST_KEY_PATH="$dev_dir/host_key" \
go run ./cmd/ssh-farm
```

In a second shell, change to `scripts/tui-tools`. Use Node.js 20+, DejaVu Sans Mono, and FFmpeg (for GIF output). Install the locked dependencies and browser, then record and replay a scene:

```sh
npm ci --ignore-scripts
npx playwright-core install chromium
ssh-keygen -q -t ed25519 -N '' -f /tmp/arcade-capture-key  # once, for development
node tui.mjs record 2223 /tmp/arcade-capture-key scene.json /tmp/scene.cast
node tui.mjs render /tmp/scene.cast /tmp/screen.png
node tui.mjs render /tmp/scene.cast /tmp/motion.gif 8
```

Set `CHROMIUM` to the absolute executable path to use an existing Chromium installation and skip the browser download. The recorder opens a loopback SSH PTY with the supplied key and plays input against xterm.js. The PNG contains the final screen. The GIF samples the recording at the selected frames per second. Replay the same cast with the same font and browser for repeatable visual comparisons, independent of live clocks and game state.

Copy `scene.json` to change terminal columns, rows, username (`user`), and input steps. A step can wait (`wait_ms`), conditionally run if screen text matches a regular expression (`when`), send literal input (`send`), and wait for a screen match (`expect`) or disappearance (`gone`). Each assertion has a 10-second timeout, configurable with `timeout_ms`. Use a target that first appears after the key for input latency measurements. A missed target exits with the last terminal screen. The recording stores terminal output; use disposable accounts and fixtures.

The JSON report contains received PTY bytes, duration, matched text, and elapsed time until the terminal buffer contained that text. A wait-only step counts output during an idle period. Terminal RTT in Help measures the query and reply through the terminal; it includes SSH and application scheduling. A terminal that ignores cursor reports shows `unavailable`.

## Test a delayed connection

Run `node latency.mjs 3322 2223 300` in another shell, then record the same scene using port 3322. The loopback relay adds 150 ms in each direction to every connection, including interactive OpenSSH clients. Repeat the scene directly and with 50, 150 and 300 ms added RTT. Compare the same step across runs. Use the same initial save and terminal size. This models propagation delay on an ordered stream; packet loss and bandwidth limits require separate network emulation.

## Development scenarios

From the repository root, run `go test ./internal/sim -run '^$' -bench BenchmarkFarmCatchUp -benchmem` to compare reconnects after one hour, a week and a year. Each sample clones the same planted save with four scarecrows and advances its clock.
