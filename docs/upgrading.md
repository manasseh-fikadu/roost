# Upgrading, backups and rolling back

Bield keeps everything it needs in its data directory: the database, the embedded node's identity and Bield's SSH key. That is `/var/lib/bield` for the [install script](install.md), `/data` in [Docker](docker.md), and `~/.config/bield` if you run the binary yourself (`--data-dir` or `BIELD_DATA_DIR` changes it).

## Upgrading

- **Install script:** run the install command again. It saves a backup before it changes anything, replaces the binary and restarts the service. `BIELD_VERSION=v0.2.0` picks a release, and the latest is the default. If that version is already running, it does nothing.
- **Docker:** `docker compose pull && docker compose up -d`, after you [save a backup](#backing-up).
- **Release archive:** stop Bield, replace the binary with the new one, and start it.

When the new version starts, it applies the database changes it brings, which are numbered files that run in a single transaction, so a failed upgrade leaves the database as it was. Before it does, it copies the database to `bield.db.bak` in the data directory. Each upgrade that changes the database replaces that copy, and one that doesn't leaves it alone, so for a backup you can count on, use `bield backup`.

Bield used to keep every sample of a device's metrics and pings for as long as the history setting said, a week by default. Now it keeps the samples of the last 48 hours, and averages of 5 minutes and of an hour for the rest, up to 90 days. The first start of a version that does this folds the old samples into averages, which takes seconds, and deletes the ones older than 48 hours. Charts of a day or more are made of the averages from then on.

A Bield that finds a database written by a newer version refuses to start and says so: `the database was written by a newer version of Bield`. It never tries to use it, since it doesn't know what changed. See [Rolling back](#rolling-back).

## Backing up

```sh
bield backup                          # bield-backup-<date>.tar.gz, in the current directory
bield backup /backups/bield.tar.gz    # a file you choose, which must not exist yet
bield backup - > bield-backup.tar.gz  # to standard output
```

It works while Bield is running, and takes a consistent copy of the database through SQLite, not by copying files that are changing. The archive holds:

- `bield.db`, the database. It includes the settings, the notification channels' secrets, the history and the activity log.
- `tailscale/`, the embedded node's identity. Without it, a restored Bield joins the tailnet as a new machine and has to be logged in again.
- `ssh/`, Bield's SSH key. Without it, Bield can't log in to the machines that trust it.

It leaves out `bield.db.bak` and the `backups` directory. The file is readable only by its owner, but it holds keys and secrets, so keep it somewhere private.

Where the data directory is depends on how you run Bield:

```sh
sudo -u bield env BIELD_DATA_DIR=/var/lib/bield bield backup /var/lib/bield/backups/bield-$(date +%F).tar.gz   # install script
docker compose exec bield bield backup - > bield-backup.tar.gz                                                # Docker
```

To back up on a schedule, put the first command in a cron job or a systemd timer, and delete old files yourself. The install script saves one before every upgrade in `/var/lib/bield/backups`, and keeps the newest five.

## Restoring

To get a backup back, stop Bield, clear the old database from the data directory, extract the backup into it, and start Bield. This also moves Bield to another machine, where the directory is new and there is nothing to clear.

```sh
sudo systemctl stop bield
sudo -u bield rm -f /var/lib/bield/bield.db /var/lib/bield/bield.db-wal /var/lib/bield/bield.db-shm
sudo -u bield tar -xzf bield-backup.tar.gz -C /var/lib/bield
sudo systemctl start bield
```

The `-wal` and `-shm` files belong to the database you are replacing, and SQLite would apply them to the restored one, so they have to go. Extract as the user Bield runs as, so it owns the files, since a backup carries no owners. In Docker, a throwaway container does the same to the volume:

```sh
docker compose down
docker run --rm -v bield_bield-data:/data -v "$PWD":/backup:ro busybox sh -c \
  'rm -f /data/bield.db /data/bield.db-wal /data/bield.db-shm && tar -xzf /backup/bield-backup.tar.gz -C /data && chown -R 65532:65532 /data'
docker compose up -d
```

The volume is called `<project>_bield-data`; `docker volume ls` shows it.

## Rolling back

Going back to an older version takes two things, since the older Bield can't read a database that a newer one has changed: the older binary, and a copy of the database from before the upgrade. Anything Bield recorded after the upgrade is lost with it: the history, the alerts and any changes in the activity log.

The install script saved the copy before the upgrade, in `/var/lib/bield/backups/bield-before-<new version>-<time>.tar.gz`. Put its database back, and install the old version:

```sh
sudo systemctl stop bield
cd /var/lib/bield/backups && ls -t     # find the backup from before the upgrade
sudo -u bield rm -f /var/lib/bield/bield.db-wal /var/lib/bield/bield.db-shm
sudo -u bield tar -xzf bield-before-v0.2.0-20261101-094749.tar.gz -C /var/lib/bield bield.db
curl -fsSL https://raw.githubusercontent.com/manasseh-fikadu/bield/main/install.sh | sudo BIELD_VERSION=v0.1.0 bash
```

Remove the `-wal` and `-shm` files first, for the reason given under [Restoring](#restoring). Extracting only `bield.db` leaves the node and the key as they are.

In Docker, pin the older tag in the compose file (`image: ghcr.io/manasseh-fikadu/bield:0.1.0`), and restore just the database from the backup you saved before the upgrade:

```sh
docker compose down
docker run --rm -v bield_bield-data:/data -v "$PWD":/backup:ro busybox sh -c \
  'rm -f /data/bield.db-wal /data/bield.db-shm && tar -xzf /backup/bield-backup.tar.gz -C /data bield.db && chown 65532:65532 /data/bield.db'
docker compose up -d
```

If you have no backup from before the upgrade, `bield.db.bak` in the data directory is the database as it was before the last upgrade that changed it. Use it only if it is from the upgrade you are undoing, which its date and the log line `backed up the database before migrating` tell you. Move it over `bield.db`, and remove the `-wal` and `-shm` files as above.
