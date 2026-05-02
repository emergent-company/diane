#!/usr/bin/env bash
# POC: Memory Platform Document Upload & Extraction (v2 — fixed field path)
# Usage: bash scripts/poc-document-extraction.sh [file-to-upload]

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

source "$PROJECT_DIR/.env.local" 2>/dev/null || true
SERVER_URL="${DIANE_SERVER_URL:-https://memory.emergent-company.ai}"
TOKEN="${DIANE_TOKEN}"
PROJECT="${DIANE_PROJECT}"

if [ -z "$TOKEN" ] || [ -z "$PROJECT" ]; then
  echo "❌ Missing DIANE_TOKEN or DIANE_PROJECT in .env.local"
  exit 1
fi

echo "=== MP Document API POC ==="
echo "Server: $SERVER_URL"
echo "Project: $PROJECT"
echo ""

# ----- Step 1: List existing documents -----
echo "--- Step 1: List existing documents ---"
RESP=$(curl -s -X GET "$SERVER_URL/api/documents?limit=5" \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Project-ID: $PROJECT")
echo "$RESP" | python3 -m json.tool 2>/dev/null
echo ""

# ----- Step 2: Upload a file -----
echo "--- Step 2: Upload a file ---"
UPLOAD_FILE="${1:-}"
if [ -z "$UPLOAD_FILE" ]; then
  UPLOAD_FILE="/tmp/poc-test-document.txt"
  echo "This is a test document for Memory Platform extraction POC.

Meeting Notes: Product Strategy — May 2, 2026
Attendees: mcj (CEO), Bob (Product Manager)

Topics discussed:
1. Mcj is working on the Diane personal AI assistant project at emergent-company.
2. Bob proposed a new feature for document extraction from uploaded files.
3. The team decided to use Memory Platform's built-in extraction capabilities.
4. Mcj uses mcj-mini (MacBook) as his primary development machine.

Action Items:
- Task-1: Research MP document upload API — assigned to mcj
- Task-2: Implement extraction summary viewer — assigned to Mike
- Task-3: Set up automated testing — assigned to Bob" > "$UPLOAD_FILE"
  echo "(Created sample file at $UPLOAD_FILE)"
fi

echo "Uploading: $UPLOAD_FILE ($(wc -c < "$UPLOAD_FILE") bytes)"
UPLOAD_RESP=$(curl -s -X POST "$SERVER_URL/api/documents/upload" \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Project-ID: $PROJECT" \
  -F "file=@$UPLOAD_FILE" \
  -F "source_type=poc_test")
echo "Upload response:"
echo "$UPLOAD_RESP" | python3 -m json.tool
echo ""

# Extract document ID from the nested structure
DOC_ID=$(echo "$UPLOAD_RESP" | python3 -c "
import json,sys
d=json.load(sys.stdin)
doc = d.get('document', {})
print(doc.get('id', ''))
" 2>/dev/null || echo "")

if [ -z "$DOC_ID" ]; then
  echo "❌ Upload failed — no document ID returned"
  exit 1
fi
echo "✅ Document ID: $DOC_ID"
echo ""

# ----- Step 3: Poll for extraction completion -----
echo "--- Step 3: Poll for extraction (up to 60s) ---"
MAX_POLL=20
for i in $(seq 1 "$MAX_POLL"); do
  sleep 3
  DOC_RESP=$(curl -s -X GET "$SERVER_URL/api/documents/$DOC_ID" \
    -H "Authorization: Bearer $TOKEN" \
    -H "X-Project-ID: $PROJECT")
  
  # Parse fields
  PROCESSING=$(echo "$DOC_RESP" | python3 -c "
import json,sys
d=json.load(sys.stdin)
print(d.get('processingStatus', 'unknown'))
print(d.get('extractionStatus', 'unknown'))
print(d.get('objectsCreated', 0))
" 2>/dev/null || echo "unknown\nunknown\n0")
  
  STATUS=$(echo "$PROCESSING" | sed -n '1p')
  EXTRACT=$(echo "$PROCESSING" | sed -n '2p')
  OBJS=$(echo "$PROCESSING" | sed -n '3p')
  
  echo "  Poll $i: processingStatus=$STATUS, extractionStatus=$EXTRACT, objectsCreated=$OBJS"
  
  if [ "$EXTRACT" = "completed" ]; then
    echo "✅ Extraction completed!"
    break
  fi
  if [ "$EXTRACT" = "failed" ] || [ "$PROCESSING" = "failed" ]; then
    echo "❌ Extraction failed"
    break
  fi
done
echo ""

# ----- Step 4: Full document metadata -----
echo "--- Step 4: Document metadata ---"
echo "$DOC_RESP" | python3 -m json.tool
echo ""

# ----- Step 5: Extraction summary -----
echo "--- Step 5: Extraction summary ---"
SUMMARY_RESP=$(curl -s -X GET "$SERVER_URL/api/documents/$DOC_ID/extraction-summary" \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Project-ID: $PROJECT")
echo "$SUMMARY_RESP" | python3 -m json.tool
echo ""

# ----- Step 6: Extraction jobs for this document -----
echo "--- Step 6: Extraction jobs ---"
JOBS_RESP=$(curl -s -X GET "$SERVER_URL/api/monitoring/extraction-jobs?source_id=$DOC_ID&limit=10" \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Project-ID: $PROJECT")
echo "$JOBS_RESP" | python3 -m json.tool
echo ""

# ----- Step 7: Document content -----
echo "--- Step 7: Document content (extracted text) ---"
CONTENT_RESP=$(curl -s -X GET "$SERVER_URL/api/documents/$DOC_ID/content" \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Project-ID: $PROJECT")
echo "$CONTENT_RESP" | python3 -c "
import json,sys
try:
    d=json.load(sys.stdin)
    content = d.get('content','')
    print(content[:500])
    if len(content) > 500:
        print(f'... ({len(content)} total chars)')
except:
    print(sys.stdin.read()[:500])
" 2>/dev/null
echo ""

# ----- Step 8: Search for extracted graph objects -----
echo "--- Step 8: Search for extracted objects (recent) ---"
SEARCH_RESP=$(curl -s -X POST "$SERVER_URL/api/graph/objects/search" \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Project-ID: $PROJECT" \
  -H "Content-Type: application/json" \
  -d '{"limit": 20, "order_by": "created_at", "order": "desc"}')
echo "$SEARCH_RESP" | python3 -c "
import json,sys
d=json.load(sys.stdin)
items = d.get('objects', d.get('data', d.get('items', [])))
for obj in items[:15]:
    obj_type = obj.get('type', obj.get('objectType', '?'))
    name = obj.get('name', obj.get('title', obj.get('properties',{}).get('name', '?')))
    cid = obj.get('id','?')[:8]
    created = obj.get('createdAt', obj.get('created_at', ''))[:19] if obj.get('createdAt', obj.get('created_at')) else ''
    print(f'  [{obj_type:20s}] {str(name):30s} id={cid}... created={created}')
total = d.get('total', d.get('totalCount', len(items)))
print(f'Total: {total}')
" 2>/dev/null
echo ""

# ----- Step 9: List docs again to confirm -----
echo "--- Step 9: List documents (confirm) ---"
FINAL_LIST=$(curl -s -X GET "$SERVER_URL/api/documents?limit=5" \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Project-ID: $PROJECT")
echo "$FINAL_LIST" | python3 -c "
import json,sys
d=json.load(sys.stdin)
for doc in d.get('documents', []):
    print(f\"  {doc['filename']:40s} | status={doc.get('processingStatus','?'):12s} | extraction={doc.get('extractionStatus','?'):12s} | objects={doc.get('objectsCreated',0):3d} | rels={doc.get('relationshipsCreated',0):3d}\")
print(f'Total: {d.get(\"total\",0)}')
"

echo ""
echo "=== POC Complete ✅ ==="
echo "Document: $DOC_ID"
echo "Extraction status: $EXTRACT"
