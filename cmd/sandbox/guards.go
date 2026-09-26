package sandbox

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/pterm/pterm"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

// Guards for platform behaviour that surprises people. Each one traces to
// a tracked issue, and each one exists because the failure it prevents is
// silent: data that never lands, a clone that runs against an empty
// directory, a preview URL that a browser refuses. A warning at the moment
// of the action costs far less than the debugging session it replaces.

// diskMountBlocksFileAPI reports the mount path that would swallow remote,
// or "" when the path is safe to move through the file API.
//
// Writing into an S3 disk mount through the file API crashes the mount and
// loses the object (issue #71). The write looks like it worked, so nothing
// tells the user until the data is missing. Reading is equally unsafe, so
// both push and pull consult this.
//
// It fails CLOSED. If the disk list cannot be read, the mount state is
// unknown, and "unknown" is not "safe": carrying on risks destroying data
// the user believes they just saved, while refusing costs them a retry.
// The two outcomes are not comparable, so the unknown case refuses.
func diskMountBlocksFileAPI(ctx context.Context, client *api.SandboxClient, sandboxID, remote string) (string, error) {
	disks, err := client.ListSandboxDisks(ctx, sandboxID)
	if err != nil {
		return "", fmt.Errorf(
			"could not check whether %s is inside an S3 disk mount on %s: %w\n\n  Moving a file into a disk mount through the file API crashes the mount\n  and loses the object (issue #71), so this stops rather than risk it.\n  Retry, or copy from inside the sandbox:\n    createos sandbox exec %s -- bash -lc 'cp ...'",
			remote, sandboxID, err, sandboxID)
	}
	clean := path.Clean(remote)
	for _, d := range disks {
		mount := path.Clean(strings.TrimSpace(d.MountPath))
		if mount == "" || mount == "." || mount == "/" {
			continue
		}
		if clean == mount || strings.HasPrefix(clean, mount+"/") {
			return mount, nil
		}
	}
	return "", nil
}

// diskMountFileAPIError is the refusal. This is a hard stop rather than a
// warning: the documented outcome is a crashed mount and a lost object, so
// carrying on would destroy data the user believes they just saved.
func diskMountFileAPIError(remote, mount, verb string) error {
	inner := "cp /local/file " + remote
	if verb == "pull" {
		inner = "cp " + remote + " /tmp/copy"
	}
	return fmt.Errorf(
		"%s is inside the S3 disk mounted at %s, and the file API cannot move data through a disk mount (issue #71)\n\n  The transfer would crash the mount and lose the object.\n  Do it from inside the sandbox instead:\n    createos sandbox exec <sandbox> -- bash -lc '%s'",
		remote, mount, inner)
}

// warnForkDropsDisks says what a fork will silently not carry.
//
// A forked sandbox comes up without its source's disk attachments (issue
// #63), so a job on the clone reads an empty directory and can "pass"
// against nothing at all.
func warnForkDropsDisks(ctx context.Context, client *api.SandboxClient, srcID string) {
	disks, err := client.ListSandboxDisks(ctx, srcID)
	if err != nil || len(disks) == 0 {
		return
	}
	mounts := make([]string, 0, len(disks))
	for _, d := range disks {
		mounts = append(mounts, d.Name+" at "+d.MountPath)
	}
	pterm.Warning.Printfln(
		"The fork will come up WITHOUT the %d disk(s) this sandbox has mounted (issue #63):\n    %s\n  Re-attach them after the fork resumes, or it will read empty directories.",
		len(disks), strings.Join(mounts, "\n    "))
}

// warnIngressCaveats fires when a public HTTPS URL is switched on. Both
// caveats cost real debugging time and neither is visible from the URL.
func warnIngressCaveats() {
	pterm.Warning.Println("The public URL has two known limits:")
	fmt.Println("    TLS is a self-signed certificate, so browsers reject it (issue #46).")
	fmt.Println("    The ingress hop strips the Authorization header, so services gated")
	fmt.Println("    on Basic or Bearer auth see no credentials (issue #64).")
}
