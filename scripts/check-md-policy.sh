#!/bin/sh
# Markdown policy: only these .md files may be tracked in this repository.
#
#   README.md, CHANGELOG.md, SECURITY.md
#   doc/FILESYSTEM-ULTRA-USAGE.md, doc/MCP-PROXY.md
#   any .md under examples/, scripts/, tests/, cmd/proxy/,
#   .claude/skills/filesystem-ultra-tools/ and the website
#
# Anything else is rejected. The matching ignore rules live in .gitignore
# and CI runs this script on every push.
set -eu

violations=0

for file in $(git ls-files '*.md'); do
	case "$file" in
	README.md | CHANGELOG.md | SECURITY.md) continue ;;
	doc/FILESYSTEM-ULTRA-USAGE.md | doc/MCP-PROXY.md) continue ;;
	examples/* | scripts/* | tests/* | cmd/proxy/*) continue ;;
	.claude/skills/filesystem-ultra-tools/*) continue ;;
	website/* | docs-website/*) continue ;;
	esac
	printf 'markdown policy violation: %s\n' "$file"
	violations=$((violations + 1))
done

if [ "$violations" -ne 0 ]; then
	printf '\n%s forbidden .md file(s) are tracked. Allowed:\n' "$violations"
	printf '  README.md, CHANGELOG.md, SECURITY.md\n'
	printf '  doc/FILESYSTEM-ULTRA-USAGE.md, doc/MCP-PROXY.md\n'
	printf '  any .md under examples/, scripts/, tests/, cmd/proxy/, website/\n'
	printf '  .claude/skills/filesystem-ultra-tools/\n'
	printf 'Move the content to the website or untrack the file.\n'
	exit 1
fi

printf 'markdown policy: ok\n'
