# CreateOS template for Amp orbs. `createos sandbox setup amp` builds it on first spawn.
# Templates may only start FROM nodeops/sandbox:* and take no COPY/ADD, so this
# rebuilds the Amp orb image contract (ampcode.com/docs/orbs/self-hosted#bring-your-own-image)
# with RUN. Desktop's Wayland stack is left out.
FROM nodeops/sandbox:debian

RUN export DEBIAN_FRONTEND=noninteractive \
	&& apt-get update \
	&& apt-get install -y --no-install-recommends \
		bash ca-certificates coreutils curl gh git nodejs openssh-client procps \
		ripgrep sudo tmux util-linux \
	&& rm -rf /var/lib/apt/lists/*

RUN id user >/dev/null 2>&1 || useradd -m -s /bin/bash user \
	&& echo "user ALL=(ALL) NOPASSWD: ALL" >/etc/sudoers.d/user \
	&& chmod 440 /etc/sudoers.d/user

# HOME too, so the checksum-verifying installer edits no real shell profile.
RUN tmp=$(mktemp -d) \
	&& curl -fsSL https://ampcode.com/install.sh | HOME="$tmp" AMP_HOME="$tmp" bash \
	&& install -m 755 "$(readlink -f "$tmp/bin/amp")" /usr/local/bin/amp \
	&& rm -rf "$tmp" \
	&& amp --version
