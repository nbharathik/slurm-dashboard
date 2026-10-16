ARG BASE=ubuntu:24.04
FROM ${BASE}

ENV DEBIAN_FRONTEND=noninteractive \
    LANG=C.UTF-8 \
    TERM=xterm-256color \
    SLURM_CONF=/etc/slurm/slurm.conf

RUN printf '#!/bin/sh\nexit 101\n' > /usr/sbin/policy-rc.d \
 && chmod +x /usr/sbin/policy-rc.d \
 && apt-get update \
 && apt-get install -y --no-install-recommends \
      slurm-wlm slurmdbd munge mariadb-server mariadb-client \
      tmux procps python3 stress-ng ca-certificates \
 && rm -rf /var/lib/apt/lists/*

RUN id slurm >/dev/null 2>&1 || useradd -r -m -d /var/lib/slurm -s /usr/sbin/nologin slurm

RUN (userdel -r ubuntu 2>/dev/null || true) \
 && (groupdel ubuntu 2>/dev/null || true) \
 && for spec in you:1000 alice:1001 bob:1002 carol:1003; do \
      u=${spec%%:*}; n=${spec##*:}; \
      groupadd -g "$n" "$u" && useradd -m -u "$n" -g "$n" -s /bin/bash "$u"; \
    done

# Test-only munge key, shared by every container built from this image.
RUN dd if=/dev/urandom of=/etc/munge/munge.key bs=1 count=1024 2>/dev/null \
 && chown munge:munge /etc/munge/munge.key \
 && chmod 0400 /etc/munge/munge.key

# Dummy GPU device files for gres.conf (Slurm only needs them to exist).
RUN mkdir -p /opt/sdash/fakegpu \
 && for i in 0 1 2 3; do \
      mknod /opt/sdash/fakegpu/nvidia$i c 1 3 2>/dev/null || touch /opt/sdash/fakegpu/nvidia$i; \
    done

RUN dpkg-query -W -f='${Version}\n' slurm-wlm > /etc/sdash-slurm-package

COPY etc/ /etc/slurm/
RUN chown slurm:slurm /etc/slurm/slurmdbd.conf && chmod 0600 /etc/slurm/slurmdbd.conf

COPY entrypoint.sh seed.sh lib.sh cli-checks.sh tui-smoke.sh /opt/sdash/
RUN chmod +x /opt/sdash/*.sh && ln -s /opt/build/sdash /usr/local/bin/sdash
