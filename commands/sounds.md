---
description: List available notification sounds and preview them
disable-model-invocation: true
allowed-tools: Bash
---

# List Available Notification Sounds

Show the user what notification sounds are available on their system.

## Step 1: Locate and run list-sounds

```bash
# Get plugin root directory
PLUGIN_ROOT="${CLAUDE_PLUGIN_ROOT}"
if [ -z "$PLUGIN_ROOT" ]; then
  INSTALLED_PATH="$HOME/.claude/plugins/marketplaces/agent-notifications-go"
  if [ -d "$INSTALLED_PATH" ]; then
    PLUGIN_ROOT="$INSTALLED_PATH"
  else
    PLUGIN_ROOT="$(pwd)"
  fi
fi

# Try the list-sounds binary
if [ -f "${PLUGIN_ROOT}/bin/list-sounds" ]; then
  "${PLUGIN_ROOT}/bin/list-sounds"
elif [ -f "${PLUGIN_ROOT}/bin/list-sounds-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')" ]; then
  "${PLUGIN_ROOT}/bin/list-sounds-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')"
else
  # Fallback: list sounds manually
  echo "Built-in sounds:"
  echo ""
  if [ -d "${PLUGIN_ROOT}/sounds" ]; then
    ls -1 "${PLUGIN_ROOT}/sounds/"*.mp3 2>/dev/null | while read file; do
      name=$(basename "$file" .mp3)
      echo "  ${name}.mp3"
    done
  else
    echo "  (sounds directory not found)"
  fi

  echo ""

  # System sounds
  case "$(uname -s)" in
    Darwin)
      if [ -d "/System/Library/Sounds" ]; then
        echo "System sounds (macOS):"
        echo ""
        ls -1 /System/Library/Sounds/*.aiff 2>/dev/null | while read file; do
          echo "  $(basename "$file" .aiff)"
        done
      fi
      ;;
    Linux)
      if [ -d "/usr/share/sounds" ]; then
        echo "System sounds (Linux):"
        echo ""
        find /usr/share/sounds -type f \( -name "*.ogg" -o -name "*.wav" \) 2>/dev/null | head -20 | while read file; do
          echo "  $(basename "$file")"
        done
      fi
      ;;
  esac
fi
```

## Step 2: Offer to preview

If the user wants to preview a sound, use the sound-preview binary or list-sounds --play:

```bash
PLUGIN_ROOT="${CLAUDE_PLUGIN_ROOT}"
if [ -z "$PLUGIN_ROOT" ]; then
  INSTALLED_PATH="$HOME/.claude/plugins/marketplaces/agent-notifications-go"
  if [ -d "$INSTALLED_PATH" ]; then
    PLUGIN_ROOT="$INSTALLED_PATH"
  else
    PLUGIN_ROOT="$(pwd)"
  fi
fi

# Use list-sounds --play for playback
"${PLUGIN_ROOT}/bin/list-sounds" --play "<sound_name>" --volume 0.3
```

After showing the list, tell the user:
- They can preview any sound by asking "play <name>"
- To configure sounds, use `/agent-notifications-go:settings`
