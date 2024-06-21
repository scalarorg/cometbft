#!/bin/sh
DIR="$( cd "$( dirname "$0" )" && pwd )"

evmos_up() {
    docker-compose -f ${DIR}/docker-compose-evmos.yml --env-file ${DIR}/.env up -d
}

evmos_down() {
    docker-compose -f ${DIR}/docker-compose-evmos.yml down
}

$@