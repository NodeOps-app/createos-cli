package sandbox

import (
	"github.com/urfave/cli/v2"
)

// newDCCommand wires up `createos sandbox dc` — Docker Compose against a
// remote devbox sandbox. The mental model: one sandbox = one Docker
// host. We don't translate compose to fc-spawn primitives — we sync your
// project into the VM, run `docker compose` inside it, and forward the
// ports back.
//
// Lifecycle:
//
//	dc up      → ensure sandbox + sshd + dockerd + Mutagen sync + `docker compose up -d`
//	             + port-forward every published port
//	dc down    → destroy the sandbox (use --keep to stop compose only)
//	dc ps      → list services + ports + health
//	dc logs    → tail compose logs
//	dc exec    → docker compose exec into a service
//
// State per project lives in `.createos/dc.lock` next to the compose
// file (sandbox id, ssh key path, port map, Mutagen session id).
func newDCCommand() *cli.Command {
	return &cli.Command{
		Name:    "dc",
		Aliases: []string{"compose"},
		Usage:   "Run docker-compose against a remote sandbox (dev loop)",
		Description: `Treats a fc-spawn sandbox as a remote Docker host. Reads your
docker-compose.yml, syncs the project directory in, runs
'docker compose up' inside the VM, and forwards published ports back to
your laptop. Edit locally, Mutagen mirrors changes, containers pick them
up natively. 'sb dc pause'/'resume' freeze the whole stack to R2.

Bind mounts (./src:/app) work — they reference the synced project copy
inside the VM. For stateful services (postgres, redis, mysql) use named
docker volumes instead so the data stays in the VM and out of your
laptop's sync loop.

Subcommands:
  up       Bring the stack up
  down     Destroy the sandbox (or just 'docker compose down' with --keep)
  ps       List services and forwarded ports
  logs     Tail compose logs
  exec     Run a command inside a service container`,
		Subcommands: []*cli.Command{
			newDCUpCommand(),
			newDCDownCommand(),
			newDCPsCommand(),
			newDCLogsCommand(),
			newDCExecCommand(),
		},
	}
}
