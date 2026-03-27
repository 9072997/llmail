# llmail

An [MCP](https://modelcontextprotocol.io/) server that gives LLMs access to your email over IMAP. Also ships with a built-in chat client for standalone use without an external MCP host.

Giving an LLM access to your email is inherently risky. See [SAFETY.md](SAFETY.md) for the full threat model and recommendations.

## MCP Server

The core of llmail is an MCP server that exposes your email as tools over stdio. Connect it to Claude Desktop, Claude Code, or any MCP-compatible client.

### Features

- **Multi-account IMAP** with connection pooling
- **Gmail extensions** -- native X-GM-RAW search and label support
- **Local full-text search** via [Bleve](https://blevesearch.com/) with background sync
- **Server-side SORT and THREAD** when the IMAP server supports it
- **Message management** -- move, copy, trash, flag, draft, edit, unsubscribe
- **Prompt injection guard** running on the CPU using Meta's Prompt Guard 2 (DeBERTa classifier)
- **Secure credential storage** via system keyring or encrypted config
- **Named profiles** for isolated config, credentials, and index per use case

### Tools

| Tool | Description |
|------|-------------|
| `list_accounts` | List configured email accounts |
| `list_folders` | List folders in an account |
| `imap_search` | Server-side IMAP search |
| `gmail_search` | Gmail-native search (X-GM-RAW) |
| `search_local_index` | Full-text search over the local Bleve index |
| `list_messages` | List messages in a folder |
| `get_message` | Fetch a message with configurable detail level |
| `get_attachment` | Download an attachment |
| `save_attachment` | Save an attachment to disk |
| `get_thread` | Fetch a full message thread |
| `move_messages` | Move messages between folders |
| `copy_messages` | Copy messages to another folder |
| `delete_messages` | Move messages to trash (permanent deletion is not supported) |
| `set_flags` | Set/clear message flags (read, starred, etc.) |
| `create_draft` | Create a new draft email (must be sent manually from an email client) |
| `edit_message` | Edit a message previously created by llmail |
| `create_folder` | Create a new folder |
| `rename_folder` | Rename a folder |
| `trash_folder` | Move all messages in a folder to trash, then remove the empty folder |
| `unsubscribe` | One-click unsubscribe via List-Unsubscribe header |
| `index_status` | Show local index sync status |
| `help` | Search syntax reference and usage topics |

## Built-in Chat Client

For standalone use, llmail includes an interactive chat client with streaming and tool calling. This mode requires an LLM provider to be configured.

Supported providers:
- **Anthropic** (Claude)
- **OpenAI**
- **OpenRouter**
- **Ollama**
- **Any OpenAI-compatible API** via custom base URL

```
llmail chat
```

## Safety Model

llmail operates over IMAP only -- it **cannot send email**. The `create_draft` tool saves drafts to your Drafts folder; you must open your email client and send them yourself.

**What the LLM can do:**
- Read, search, and fetch messages and attachments
- Download attachments to a local file (can't overwrite files)
- Move and copy messages between folders
- Trash messages (moves to Trash -- permanent deletion is not supported)
- Create and rename folders; trash folders (moves contents to Trash, then removes the empty folder)
- Set/clear flags (read, starred, etc.)
- Create and edit draft messages (editing is restricted to messages llmail created, identified by an `X-LLMail-Created` header)
- Unsubscribe from mailing lists via RFC 2369/8058 headers

**What it cannot do:**
- Send email (IMAP is a read/store protocol, not a submission protocol)
- Permanently delete messages or empty your trash
- Edit messages it did not create

A misbehaving LLM could make a mess of your folders and flags, or move messages to trash in bulk, but it cannot permanently destroy data. Your email provider's trash retention policy is the safety net. The optional prompt injection guard (Meta Prompt Guard 2) adds a rudimentary layer of protection against malicious instructions embedded in email content.

**Data exfiltration is the primary risk.** Even without send access, a prompt-injected LLM can leak email content through drafts, unsubscribe side-channels, or other MCP tools the agent has access to. See [SAFETY.md](SAFETY.md) for the full threat model and recommendations.

## Installation

```
go install github.com/jpennington/llmail/cmd/llmail@latest
```

## Quick Start

1. **Run the setup wizard:**

   ```
   llmail setup
   ```

   This walks you through adding an IMAP account and optionally enabling the local search index, prompt injection guard, and LLM provider (for the built-in chat client).

2. **Use as an MCP server** (e.g. with Claude Desktop):

   ```json
   {
     "mcpServers": {
       "llmail": {
         "command": "llmail",
         "args": ["serve"]
       }
     }
   }
   ```

3. **Or use the built-in chat client** (requires an LLM provider in config):

   ```
   llmail chat
   ```

## Commands

```
llmail serve      Start the MCP server (stdio transport)
llmail chat       Interactive chat with tool-calling LLM
llmail setup      Interactive setup wizard
llmail status     Show account and index info
llmail reindex    Rebuild the local search index
llmail guard      Manage the prompt injection guard model
```

## Configuration

Config lives at `$XDG_CONFIG_HOME/llmail/config.yaml` (typically `~/.config/llmail/config.yaml`). The setup wizard handles creation, but here is the structure:

```yaml
accounts:
  personal:
    host: imap.gmail.com
    port: 993
    username: you@gmail.com
    tls: true
    capabilities:
      gmail_extensions: true

# Optional: only needed for `llmail chat`
llm:
  provider: anthropic  # or openai, openrouter, ollama, openai-compatible
  model: claude-sonnet-4-20250514

# Optional: local full-text search
index:
  enabled: true
  folders:
    - INBOX

# Optional: prompt injection detection
guard:
  enabled: true
  threshold: 0.80
```

Use `--profile <name>` on any command for isolated config, credentials, and index.

## License

This program is free software; you can redistribute it and/or modify it under the terms of the GNU General Public License as published by the Free Software Foundation; either version 2 of the License, or (at your option) any later version.

See [LICENSE](LICENSE) for the full text.
