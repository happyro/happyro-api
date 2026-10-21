#!/bin/sh
set -eu
container_help() {
    cyan='\033[1;36m'; yellow='\033[1;33m'; green='\033[1;32m'; example='\033[36m'; reset='\033[0m'
    for option in "$@"; do
        if [ "$option" = '--no-color' ]; then cyan=''; yellow=''; green=''; example=''; reset=''; fi
    done
    printf '\n%bHappyRO API container%b\n\n' "$cyan" "$reset"
    printf '%bCommands%b\n' "$yellow" "$reset"
    printf '  %binitialize%b              Migrate, create the initial account and import catalogs\n' "$green" "$reset"
    printf '  %brefresh-custom-catalogs%b  Generate and atomically import all catalogs\n' "$green" "$reset"
    printf '  %bserve%b                   Start the Go API and frontend server\n' "$green" "$reset"
    printf '  %busers | migrate | init | catalog%b  Go management commands\n\n' "$green" "$reset"
    printf '%bExamples%b\n  %binitialize --no-color%b\n  %busers --help --no-color%b\n\n' "$yellow" "$reset" "$example" "$reset" "$example" "$reset"
}
if [ "$#" -eq 0 ]; then container_help; exit 0; fi
case "$1" in
    --help|-h|--no-color)
        for option in "$@"; do
            case "$option" in --help|-h|--no-color) ;; *) echo "Place the command before its options" >&2; exit 2 ;; esac
        done
        container_help "$@"; exit 0 ;;
    initialize|refresh-custom-catalogs)
        operation="$1"; shift
        help=false
        for option in "$@"; do
            case "$option" in --help|-h) help=true ;; --no-color) ;; *) echo "Unknown container command option: $option" >&2; exit 2 ;; esac
        done
        if [ "$help" = true ]; then container_help "$@"; exit 0; fi
        set -- "$operation" ;;
esac
run_api() {
    gosu www-data happyro-api "$@"
}
refresh_catalogs() {
    gosu www-data python3 /opt/happyro/catalogs.py refresh --base /opt/happyro/catalog-base \
        --server /opt/happyro/server-base --custom /opt/happyro/custom \
        --output /opt/api/resources/game-data --no-color
    run_api catalog import --client-items resources/game-data/items/client-kro-20211105.json \
        --server-items resources/game-data/items/renewal.json \
        --monsters resources/game-data/monsters/renewal.json \
        --npcs resources/game-data/world/npc-catalog.json --no-color
}
case "$1" in
    serve)
        for option in "$@"; do
            case "$option" in --help|-h) exec gosu www-data happyro-api "$@" ;; esac
        done
        mkdir -p /run/happyro-settings
        chown www-data:www-data /run/happyro-settings
        if [ -f /run/happyro-settings/battle_conf.txt ]; then
            chown www-data:www-data /run/happyro-settings/battle_conf.txt
        fi
        exec gosu www-data happyro-api "$@"
        ;;
    initialize)
        run_api migrate --no-color
        run_api init --no-color
        refresh_catalogs
        ;;
    refresh-custom-catalogs) refresh_catalogs ;;
    *) exec gosu www-data happyro-api "$@" ;;
esac
