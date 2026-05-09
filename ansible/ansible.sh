#!/usr/bin/env bash
# Diane Ansible Helper — convenience wrapper around the playbooks.
# Usage: ./ansible.sh <playbook> [extra-args...]
set -euo pipefail

cd "$(dirname "$0")"

PLAYBOOK="${1:-help}"
shift 2>/dev/null || true

case "$PLAYBOOK" in
  build|deploy|test|logs|health|override|seed)
    PLAYBOOK="plays/${PLAYBOOK}.yml"
    ;;
  help|--help|-h)
    echo "Diane Ansible Helper — manage diane-test environment"
    echo ""
    echo "Usage: ./ansible.sh <command> [args...]"
    echo ""
    echo "Commands:"
    echo "  build               Build Linux binary locally (cross-compile)"
    echo "  health              Check all services health"
    echo "  deploy              Deploy new binary to containers"
    echo "  test                Build + deploy + run integration tests"
    echo "  logs [args...]      View container logs"
    echo "  override <args...>  Manage graph agent overrides"
    echo ""
    echo "Examples:"
    echo "  ./ansible.sh build"
    echo "  ./ansible.sh health"
    echo "  ./ansible.sh test"
    echo "  ./ansible.sh deploy -e target=master"
    echo '  ./ansible.sh logs -e filter="override" -e lines=100'
    echo '  ./ansible.sh override -e agent=diane-dreamer -e state=disabled'
    echo '  ./ansible.sh override -e agent=diane-dreamer -e state=enabled'
    echo '  ./ansible.sh override -e list=true'
    echo ""
    exit 0
    ;;
  *)
    echo "Unknown command: $PLAYBOOK"
    echo "Run ./ansible.sh help for usage."
    exit 1
    ;;
esac

if [ ! -f "$PLAYBOOK" ]; then
  echo "Playbook not found: $PLAYBOOK"
  exit 1
fi

echo "═══ Running: $PLAYBOOK ═══"
ansible-playbook "$PLAYBOOK" "$@"
