# Diane Test Environment — Ansible

Ansible playbooks for managing the `diane-test` integration environment running on a homelab server in Waliły.

## Requirements

- Ansible 2.9+ (`brew install ansible`)
- `community.docker` collection (`ansible-galaxy collection install community.docker`)

## Setup

```bash
cd ansible

# Install secrets (from 1Password)
cp host_vars/diane-test.secret.yml host_vars/diane-test.secret.yml
# Edit with the actual API key

# Verify connectivity
./ansible.sh health
```

## Usage

```bash
./ansible.sh health                   # Check all services
./ansible.sh deploy                   # Build + deploy binary
./ansible.sh test                     # Run integration tests
./ansible.sh logs                     # View container logs
./ansible.sh override -e list=true    # List current graph overrides
```

### Deploy new binary

```bash
# 1. Build
cd ../server && GOOS=linux GOARCH=amd64 go build \
  -o /tmp/diane-linux-amd64 \
  -ldflags "-X main.Version=v$(git describe --tags --abbrev=0)-dev" \
  ./cmd/diane/

# 2. Deploy (replaces binary in containers, restarts)
cd ../ansible && ./ansible.sh deploy
```

### Graph overrides (disable/re-enable agents)

```bash
# Disable diane-dreamer
./ansible.sh override -e agent=diane-dreamer -e state=disabled

# Re-enable
./ansible.sh override -e agent=diane-dreamer -e state=enabled

# List current overrides
./ansible.sh override -e list=true
```

### View logs

```bash
# Last 50 lines from master
./ansible.sh logs -e target=master

# Filter by pattern
./ansible.sh logs -e filter="override|agent-watch" -e lines=200

# Both containers, all lines
./ansible.sh logs -e lines=all
```

## Architecture

```
ansible/
├── ansible.cfg              # Ansible config
├── ansible.sh               # Convenience wrapper
├── .gitignore
├── inventory/
│   └── hosts                # Inventory (diane-test via Tailscale IP)
├── host_vars/
│   ├── diane-test.yml        # Non-sensitive vars
│   └── diane-test.secret.yml # API key (gitignored)
└── plays/
    ├── deploy.yml            # Replace binary in containers
    ├── health.yml            # Service health checks
    ├── logs.yml              # Container log viewer
    ├── seed.yml              # Re-seed agents
    ├── test.yml              # Integration test runner
    └── graph-override.yml    # Graph-based agent management
```
