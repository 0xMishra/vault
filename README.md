# Vault

A Redis-compatible in-memory key-value store written in Go. Vault speaks the Redis wire protocol (RESP), so you can talk to it with `redis-cli` or any standard Redis client. It supports key expiry, AOF persistence, and basic leader/follower replication.

![Go Version](https://img.shields.io/badge/go-1.27.4-00ADD8?logo=go)
![License](https://img.shields.io/badge/license-MIT-green)
![Status](https://img.shields.io/badge/status-in%20development-orange)

> Built as a learning project to understand how Redis works internally: networking, protocol parsing, concurrency, key expiry, durability, and replication.

---

## Table of Contents

- [Features](#features)
- [Supported Commands](#supported-commands)
- [Quick Start](#quick-start)
- [Usage](#usage)
- [Configuration](#configuration)
- [Architecture](#architecture)
- [AOF Persistence](#aof-persistence)
- [Replication](#replication)
- [Project Structure](#project-structure)
- [Development](#development)
- [Testing](#testing)
- [Limitations](#limitations)
- [Roadmap](#roadmap)
- [Acknowledgements](#acknowledgements)
- [License](#license)

---

## Features

- **RESP protocol**: parses and encodes Redis Serialization Protocol messages
- **Concurrent clients**: one goroutine per connection, thread-safe store
- **Core commands**: `PING`, `ECHO`, `SET`, `GET`, `DEL`, `EXISTS`, `INCR`
- **Key expiry**: `SET ... PX <ms>` with lazy (on read) and active (background goroutine) expiration
- **AOF persistence**: every write command is appended to a log file and replayed on startup
- **Replication**: one leader, many followers, full sync followed by live command propagation
- **Compatible**: works with `redis-cli` and standard Redis client libraries

---

## Supported Commands

| Command     | Syntax                      | Description                                              |
|-------------|-----------------------------|----------------------------------------------------------|
| `PING`      | `PING`                      | Returns `PONG`                                           |
| `ECHO`      | `ECHO message`              | Returns the given message                                |
| `SET`       | `SET key value [PX ms]`     | Sets a key, optionally with an expiry in milliseconds    |
| `GET`       | `GET key`                   | Returns the value, or nil if missing or expired          |
| `DEL`       | `DEL key [key ...]`         | Deletes keys, returns the number of keys removed         |
| `EXISTS`    | `EXISTS key [key ...]`      | Returns the number of keys that exist                    |
| `INCR`      | `INCR key`                  | Increments an integer value by 1 (creates it at 0 first) |
| `INFO`      | `INFO replication`          | Returns role and replication details                     |
| `REPLICAOF` | `REPLICAOF host port`       | Makes this node a follower of the given leader           |
| `REPLCONF`  | `REPLCONF ...`              | Handshake command used between follower and leader       |
| `PSYNC`     | `PSYNC replid offset`       | Handshake command that starts a full resync              |

---

## Quick Start

### Prerequisites

- Go 1.27.4 or newer
- `make` (optional)
- `redis-cli` (optional, for testing)

### Clone and build

```bash
git clone https://github.com/0xMishra/vault.git
cd vault
make build
# or
go build -o bin/vault ./cmd/vault
```

### Run

```bash
make run
# or
./bin/vault
```

By default Vault listens on `0.0.0.0:6379` and writes its AOF to `./vault.aof`.

---

## Usage

Connect with `redis-cli`:

```bash
redis-cli -p 6379
```

```text
127.0.0.1:6379> PING
PONG
127.0.0.1:6379> SET name vault
OK
127.0.0.1:6379> GET name
"vault"
127.0.0.1:6379> SET session abc123 PX 5000
OK
127.0.0.1:6379> GET session
"abc123"
# ...5 seconds later
127.0.0.1:6379> GET session
(nil)
127.0.0.1:6379> INCR counter
(integer) 1
127.0.0.1:6379> EXISTS name counter missing
(integer) 2
127.0.0.1:6379> DEL name counter
(integer) 2
```

Or with raw RESP over `netcat`:

```bash
printf '*1\r\n$4\r\nPING\r\n' | nc localhost 6379
# +PONG
```

---

## Configuration

| Flag            | Default       | Description                                      |
|-----------------|---------------|--------------------------------------------------|
| `--port`        | `6379`        | TCP port to listen on                            |
| `--aof`         | `true`        | Enable AOF persistence                           |
| `--aof-file`    | `vault.aof`   | Path to the AOF file                             |
| `--aof-fsync`   | `everysec`    | Fsync policy: `always`, `everysec`, or `no`      |
| `--replicaof`   | (none)        | Start as a follower of `"host port"`             |

```bash
./bin/vault --port 6380 --aof-file ./data/vault.aof --aof-fsync always
```

---

## Architecture

```text
                +-----------------------------+
 redis-cli ---> |  server (TCP listener)      |
 clients        +--------------+--------------+
                               | goroutine per connection
                               v
                +-----------------------------+
                |  resp (parser / encoder)    |
                +--------------+--------------+
                               |
                               v
                +-----------------------------+
                |  command (handler/dispatch) |
                +---+-----------+---------+---+
                    |           |         |
          write cmds|           |         | write cmds
                    v           v         v
          +--------------+ +---------+ +-------------------+
          | persistence  | |  store  | |   replication     |
          | (AOF)        | | (map +  | | (leader to        |
          +--------------+ | RWMutex)| |  followers)       |
                           +----+----+ +-------------------+
                                ^
                                | sweeps expired keys
                        +-------+--------+
                        | expiry (bg)    |
                        +----------------+
```

**Request flow**

1. `server` accepts a connection and spawns a goroutine for it.
2. `resp` parses a command (an array of bulk strings).
3. `command` looks up the handler by command name.
4. For write commands (`SET`, `DEL`, `INCR`): append to the AOF, apply to the store, then propagate to followers.
5. `resp` encodes the reply and it is written back to the client.

**Concurrency model**

The store is a `map[string]entry` guarded by a `sync.RWMutex`. Reads take a read lock; writes take a write lock. `INCR` holds the write lock for its whole read-modify-write so it is atomic.

**Key expiry**

- **Lazy**: `GET` and `EXISTS` check the expiry timestamp and delete the key if it has passed.
- **Active**: a background goroutine on a `time.Ticker` periodically removes expired keys so unread keys do not leak memory.

---

## AOF Persistence

Every write command is logged so the dataset can be rebuilt after a restart. Writer: `internal/persistence/aof.go`. Startup replay: `internal/persistence/replay.go`.

### How it works

1. **Append**: each write command is serialized in RESP and appended to the AOF file.
2. **Fsync**: the file is flushed to disk according to the configured policy.
3. **Replay**: on startup, Vault reads the AOF from the beginning and re-executes each command against an empty store.

### Example AOF contents

After `SET name vault` and `INCR counter` (each line ends with `\r\n` on disk):

```text
*3
$3
SET
$4
name
$5
vault
*2
$4
INCR
$7
counter
```

### Fsync policies

| Policy     | Behavior                                  | Durability                    | Performance |
|------------|-------------------------------------------|-------------------------------|-------------|
| `always`   | Fsync after every write command           | Safest, no acknowledged loss  | Slowest     |
| `everysec` | Fsync once per second in the background   | Up to ~1 second of data lost  | Balanced    |
| `no`       | Let the OS decide when to flush           | Weakest                       | Fastest     |

### Expiry and replay

A relative expiry (`PX 5000`) would give keys a fresh TTL on every restart if replayed verbatim. Vault logs the **absolute expiry timestamp** instead, so keys that expired while the server was down are skipped on replay.

### Replay behavior

- Replay runs **before** the listener starts, so no client sees a partial dataset.
- Replayed commands are **not** re-appended to the AOF.
- A truncated final command (for example after a crash mid-write) is discarded and everything before it is loaded.

### Try it

```bash
./bin/vault &
redis-cli SET city pune
redis-cli INCR visits

kill %1
./bin/vault &

redis-cli GET city      # "pune"
redis-cli GET visits    # "1"
```

---

## Replication

Vault supports one leader with any number of followers. Code lives in `internal/replication/` and `internal/command/replication.go`.

### Start a follower

```bash
# leader
./bin/vault --port 6379

# follower
./bin/vault --port 6380 --aof-file ./follower.aof --replicaof "localhost 6379"
```

Or at runtime:

```bash
redis-cli -p 6380 REPLICAOF localhost 6379
```

### How it works

1. **Handshake**: the follower connects to the leader and sends `PING`, `REPLCONF`, then `PSYNC`.
2. **Full sync**: the leader replies with a full copy of its current dataset.
3. **Propagation**: after the sync, the leader forwards every write command (`SET`, `DEL`, `INCR`) to all followers, in order.
4. **Reads on followers**: followers apply propagated commands to their own store (and AOF) and can serve reads.

Check the role from either side:

```bash
redis-cli -p 6379 INFO replication
redis-cli -p 6380 INFO replication
```

---

## Project Structure

```text
vault/
├── cmd/
│   └── vault/
│       └── main.go            # flags, AOF replay, server start, optional --replicaof
│
├── internal/
│   ├── server/
│   │   ├── server.go          # TCP listener, accept loop
│   │   ├── connection.go      # per-connection read/dispatch/write loop
│   │   └── server_test.go
│   │
│   ├── resp/
│   │   ├── parser.go          # RESP parser
│   │   ├── encoder.go         # RESP encoder
│   │   ├── types.go           # RESP value types
│   │   ├── parser_test.go
│   │   └── encoder_test.go
│   │
│   ├── command/
│   │   ├── handler.go         # command registry and dispatcher
│   │   ├── string.go          # PING, ECHO, SET, GET, INCR
│   │   ├── key.go             # DEL, EXISTS
│   │   ├── replication.go     # INFO, REPLICAOF, REPLCONF, PSYNC
│   │   ├── string_test.go
│   │   └── key_test.go
│   │
│   ├── store/
│   │   ├── store.go           # thread-safe in-memory store
│   │   ├── entry.go           # value and expiry metadata
│   │   ├── expiry.go          # lazy and active expiry
│   │   ├── store_test.go
│   │   └── expiry_test.go
│   │
│   ├── persistence/
│   │   ├── aof.go             # AOF writer and fsync policies
│   │   ├── replay.go          # AOF replay on startup
│   │   ├── aof_test.go
│   │   └── replay_test.go
│   │
│   └── replication/
│       ├── leader.go          # follower registry, full sync, propagation
│       ├── follower.go        # handshake, sync, apply propagated commands
│       └── replication_test.go
│
├── tests/
│   └── e2e_test.go            # end-to-end tests over real TCP connections
│
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

Unit tests (`*_test.go`) sit next to the code they test and use the same package name, so they can reach unexported code. `tests/` holds only end-to-end tests.

---

## Development

```bash
make build    # compile to ./bin/vault
make run      # build and run
make test     # go test ./... -race
make fmt      # gofmt
make vet      # go vet
```

> Adjust the targets to match your actual `Makefile`.

---

## Testing

```bash
go test ./... -race
```

- **resp**: arrays, bulk strings, partial reads, malformed input, encoder output
- **store**: concurrent `SET`/`GET`/`INCR`, lazy and active expiry
- **command**: replies and error cases (`INCR` on a non-integer, missing keys, overflow)
- **persistence**: write then replay gives an identical store, expired keys skipped, truncated tail handled
- **replication**: follower catches up after full sync, writes on the leader appear on the follower
- **e2e (`tests/`)**: real TCP clients, concurrent connections, restart with AOF

Conventions:

- Server tests listen on `127.0.0.1:0` so the OS picks a free port.
- AOF tests use `t.TempDir()`.
- Keep `PX` values short and poll with a timeout instead of long sleeps.
- Always run with `-race`.

Benchmark with the official tooling:

```bash
redis-benchmark -p 6379 -t set,get -n 100000 -q
```

---

## Limitations

- One in-memory map behind one `RWMutex`, so writers contend on a single lock.
- The AOF grows without bound (no rewrite yet).
- Replication is asynchronous: a leader crash can lose writes the followers have not received.
- No automatic failover; promoting a follower is manual.
- Strings only; no other data types.
- No authentication or TLS: do not expose Vault to untrusted networks.

---

## Roadmap

### Done (MVP)

- [x] RESP parser and encoder, TCP server
- [x] `PING`, `ECHO`, `SET`, `GET`
- [x] Concurrent clients
- [x] Key expiry (`PX`) with lazy and active cleanup
- [x] `DEL`, `EXISTS`, `INCR`
- [x] AOF persistence with replay on startup
- [ ] Basic replication: leader/follower handshake, full sync, command propagation

### Out of scope for the MVP (single-node features)

- [ ] AOF rewrite and compaction
- [ ] RDB snapshots
- [ ] Transactions (`MULTI`, `EXEC`, `DISCARD`, `WATCH`)
- [ ] Streams (`XADD`, `XREAD`, consumer groups)
- [ ] Lists (`LPUSH`, `RPUSH`, `LPOP`, `RPOP`, `LRANGE`, blocking pops)
- [ ] Other data types (hashes, sets, sorted sets)
- [ ] LRU eviction and `maxmemory` policies
- [ ] More expiry commands (`EXPIRE`, `TTL`, `PERSIST`, `EX`/`NX`/`XX` on `SET`)
- [ ] Authentication (`AUTH`) and TLS
- [ ] `CONFIG` and other admin commands

### Distributed Vault (planned, after the MVP)

**Replication improvements**

- [ ] Replication offset tracking and `WAIT` for acknowledged writes
- [ ] Partial resync using a replication backlog
- [ ] Read-only followers and read scaling

**Messaging**

- [ ] Pub/Sub (`SUBSCRIBE`, `UNSUBSCRIBE`, `PUBLISH`, pattern subscriptions)
- [ ] Queue features built on lists and blocking operations

**High availability**

- [ ] Failure detection (heartbeats, timeouts)
- [ ] Leader election and automatic failover with a simple Raft implementation
- [ ] Replicated log with majority commit
- [ ] Sentinel-style monitoring and client redirection

**Sharding and clustering**

- [ ] Consistent-hashing sharding across nodes
- [ ] Hash slots (16384) and `CLUSTER` commands
- [ ] `MOVED` / `ASK` redirections
- [ ] Online rebalancing and slot migration
- [ ] Cluster membership via gossip

**Operations**

- [ ] Metrics and observability
- [ ] Chaos and partition testing for the distributed features

---

## Acknowledgements

- [CodeCrafters "Build your own Redis"](https://codecrafters.io/challenges/redis)
- [Redis protocol specification (RESP)](https://redis.io/docs/latest/develop/reference/protocol-spec/)
- [Redis persistence docs](https://redis.io/docs/latest/operate/oss_and_stack/management/persistence/)
- [Redis replication docs](https://redis.io/docs/latest/operate/oss_and_stack/management/replication/)

---

## License

This project is licensed under the MIT License. See the `LICENSE` file for details.
