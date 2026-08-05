#!/bin/sh
# Bring a local Neo4j up over a luatdo export, in podman or docker.
#
# The offline importer is the fast path and it refuses to run against a live
# database, so this is three steps and not one: pull the image, import the CSV
# dump into a volume with no server running, then start a server over that
# volume. Anything that tries to do all three at once ends up either importing
# into a database somebody is querying or starting a server it has to kill.
set -eu

usage() {
	cat <<'EOF'
usage: neo4j.sh <command>

  pull      fetch the Neo4j image
  import    load the export into the volume, with no server running
  up        start the server over the volume
  down      stop and remove the container, keeping the volume
  status    say what is running and what the database holds
  wipe      remove the volume, which throws the imported graph away
  logs      follow the server log

environment:
  LUATDO_DUMP        export directory        (default $HOME/data/luatdo/export/neo4j)
  LUATDO_NEO4J_IMAGE image                   (default docker.io/library/neo4j:5.26)
  LUATDO_NEO4J_PASS  password                (default luatdo-local)
  LUATDO_HTTP_PORT   browser port            (default 7474)
  LUATDO_BOLT_PORT   bolt port               (default 7687)
  LUATDO_HEAP        java heap               (default 1G)
  LUATDO_PAGECACHE   page cache              (default 512M)
EOF
}

DUMP=${LUATDO_DUMP:-$HOME/data/luatdo/export/neo4j}
IMAGE=${LUATDO_NEO4J_IMAGE:-docker.io/library/neo4j:5.26}
PASS=${LUATDO_NEO4J_PASS:-luatdo-local}
HTTP_PORT=${LUATDO_HTTP_PORT:-7474}
BOLT_PORT=${LUATDO_BOLT_PORT:-7687}
HEAP=${LUATDO_HEAP:-1G}
PAGECACHE=${LUATDO_PAGECACHE:-512M}

NAME=luatdo-neo4j
VOLUME=luatdo-neo4j-data
# The database name has to match the one the export baked into import.sh.
# Community edition runs a single user database and will not create a second
# one, so it is renamed through configuration rather than created through Cypher.
DB=luatdo

# podman first because it needs no daemon, docker if that is what the host has.
if command -v podman > /dev/null 2>&1; then
	RT=podman
elif command -v docker > /dev/null 2>&1; then
	RT=docker
else
	echo "neo4j.sh: neither podman nor docker is on PATH" >&2
	exit 1
fi

# The dump directory is mounted writable because the importer writes its report
# next to the data it read, and refuses to start when it cannot.
mount_dump() {
	if [ ! -f "$DUMP/import.sh" ]; then
		echo "neo4j.sh: no import.sh in $DUMP, run luatdo export neo4j first" >&2
		exit 1
	fi
	# The z suffix relabels for SELinux hosts and is ignored elsewhere.
	echo "-v $DUMP:/import:z"
}

running() { $RT ps --format '{{.Names}}' | grep -qx "$NAME"; }

case "${1:-}" in
pull)
	$RT pull "$IMAGE"
	;;
import)
	if running; then
		echo "neo4j.sh: $NAME is running, stop it with 'neo4j.sh down' before importing" >&2
		exit 1
	fi
	$RT volume create "$VOLUME" > /dev/null 2>&1 || true
	echo "importing $DUMP into volume $VOLUME as database $DB"
	# import.sh is run rather than reproduced. The export writes the exact file
	# list, the flags that make multiline Vietnamese text load, and the database
	# name, and a copy of that list here would go stale the first time a node
	# type is added.
	# shellcheck disable=SC2046
	$RT run --rm $(mount_dump) -v "$VOLUME":/data -w /import "$IMAGE" sh ./import.sh
	echo "imported, start it with 'neo4j.sh up'"
	;;
up)
	if running; then
		echo "$NAME is already up"
		exit 0
	fi
	$RT rm -f "$NAME" > /dev/null 2>&1 || true
	$RT run -d --name "$NAME" \
		-p "$HTTP_PORT":7474 -p "$BOLT_PORT":7687 \
		-v "$VOLUME":/data \
		-e NEO4J_AUTH="neo4j/$PASS" \
		-e NEO4J_initial_dbms_default__database="$DB" \
		-e NEO4J_server_memory_heap_max__size="$HEAP" \
		-e NEO4J_server_memory_pagecache_size="$PAGECACHE" \
		"$IMAGE" > /dev/null
	echo "$NAME starting on http://localhost:$HTTP_PORT and bolt://localhost:$BOLT_PORT"
	echo "user neo4j, password $PASS, database $DB"
	echo "point luatdo at it with:"
	echo "  export LUATDO_NEO4J_URI=bolt://localhost:$BOLT_PORT"
	echo "  export LUATDO_NEO4J_USER=neo4j LUATDO_NEO4J_PASSWORD=$PASS"
	echo "  export LUATDO_NEO4J_DATABASE=$DB"
	;;
down)
	$RT rm -f "$NAME" > /dev/null 2>&1 || true
	echo "$NAME removed, volume $VOLUME kept"
	;;
wipe)
	# Named separately from down because it is the one step that destroys
	# something, and a flag on a stop command is too easy to type by accident.
	$RT rm -f "$NAME" > /dev/null 2>&1 || true
	$RT volume rm "$VOLUME" > /dev/null 2>&1 || true
	echo "volume $VOLUME removed, the imported graph is gone"
	;;
status)
	if running; then
		echo "$NAME is up on http://localhost:$HTTP_PORT"
	else
		echo "$NAME is not running"
	fi
	$RT volume inspect "$VOLUME" > /dev/null 2>&1 && echo "volume $VOLUME exists" || echo "volume $VOLUME does not exist"
	;;
logs)
	$RT logs -f "$NAME"
	;;
*)
	usage
	exit 1
	;;
esac
