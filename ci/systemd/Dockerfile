# A machine with a real systemd and a user manager, to test the package's
# systemd user unit. Built with network (packages only); the test then runs
# with --network none, so gocore and the miner cannot reach anything.
FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update -qq && apt-get install -y -qq systemd systemd-sysv dbus dbus-user-session procps >/dev/null
COPY dist/core-starter_*_amd64.deb /pkg/core-starter.deb
RUN apt-get install -y -qq /pkg/core-starter.deb >/dev/null && useradd -m tester
# Never touch the host's devices, even if a package pulled in udev. Nor its
# binfmt_misc (not namespaced): systemd-binfmt would drop the host's handlers,
# e.g. Rosetta when the container runs emulated (ci-local.sh on Apple silicon).
RUN systemctl mask systemd-udevd.service systemd-udev-trigger.service systemd-udevd-control.socket systemd-udevd-kernel.socket \
    systemd-binfmt.service proc-sys-fs-binfmt_misc.automount proc-sys-fs-binfmt_misc.mount
COPY ci/systemd/test.sh /ci/test.sh
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
