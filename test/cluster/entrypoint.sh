#!/bin/bash
# Container entrypoint of the throwaway test cluster: db, ctld, node or login.
# Test use only: fixed passwords and a munge key baked into the image.
set -Eeu

role=${1:?usage: entrypoint.sh db|ctld|node|login}

dump_logs() {
	local f
	for f in /var/log/slurm/*.log; do
		[ -f "$f" ] || continue
		echo "== $f" >&2
		tail -n 40 "$f" >&2
	done
}
trap dump_logs ERR

wait_tcp() {
	until (exec 3<>"/dev/tcp/$1/$2") 2>/dev/null; do sleep 1; done
}

start_munge() {
	install -d -o munge -g munge -m 0755 /run/munge
	install -d -o munge -g munge -m 0700 /var/log/munge
	install -d -o munge -g munge -m 0711 /var/lib/munge
	chown munge:munge /etc/munge
	chmod 0700 /etc/munge
	runuser -u munge -- munged --force
	until munge -n >/dev/null 2>&1; do sleep 0.5; done
}

run_dirs() {
	install -d -o slurm -g slurm /var/spool/slurmctld /var/log/slurm /run/slurm
	install -d /var/spool/slurmd
}

role_db() {
	install -d -o mysql -g mysql /run/mysqld /var/lib/mysql
	[ -d /var/lib/mysql/mysql ] || mariadb-install-db --user=mysql --datadir=/var/lib/mysql >/dev/null
	cat >/tmp/init.sql <<'SQL'
CREATE DATABASE IF NOT EXISTS slurm_acct_db;
CREATE USER IF NOT EXISTS 'slurm'@'%' IDENTIFIED BY 'sdash-test-only';
GRANT ALL ON slurm_acct_db.* TO 'slurm'@'%';
FLUSH PRIVILEGES;
SQL
	exec mariadbd --user=mysql --bind-address=0.0.0.0 --skip-name-resolve \
		--init-file=/tmp/init.sql \
		--innodb-buffer-pool-size=4096M --innodb-lock-wait-timeout=900
}

role_ctld() {
	start_munge
	run_dirs
	wait_tcp db 3306
	slurmdbd
	until timeout 5 sacctmgr -n show cluster >/dev/null 2>&1; do sleep 1; done
	sacctmgr -i add cluster sdashtest >/dev/null 2>&1 || true
	slurmctld
	until scontrol ping 2>/dev/null | grep -q 'is UP'; do sleep 1; done
	touch /run/sdash-ready
	exec tail -F /var/log/slurm/slurmctld.log /var/log/slurm/slurmdbd.log
}

role_node() {
	start_munge
	run_dirs
	wait_tcp ctld 6817
	exec slurmd -D -N "$(hostname -s)"
}

role_login() {
	start_munge
	exec sleep infinity
}

"role_$role"
