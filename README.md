# evol

Self-improvement loop for agent capabilities: evaluate, benchmark, replay

## Install

```sh
go install hop.top/evol@latest
```

Or download a binary from
[Releases](https://hop.top/evol/releases).

## Usage

```sh
evol --help
evol --version
```

### Output formats

```sh
evol --format json
evol --format yaml
```

## Configuration

Config file: `~/.config/evol/config.yaml`

Environment variables prefixed with `evol_` are
also recognized.

## Development

Prerequisites: Go 1.23+, [Task](https://taskfile.dev)

```sh
task setup    # download deps
task check    # lint + test
task build    # build binary
```

## License

See [LICENSE](LICENSE).

---
Maintained by Jad Bitar.
