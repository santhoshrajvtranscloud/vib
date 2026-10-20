#!/usr/bin/env bash
set -e

echo "=== Testing Malformed & Edge-Case HCL Payloads ==="

# 1. Start server in background
./bin/vib --serve --port 8999 &
SERVER_PID=$!

# Ensure server cleanup on exit
trap "kill -9 $SERVER_PID 2>/dev/null || true" EXIT

# Wait for server to become ready
READY=0
for i in $(seq 1 30); do
  if curl -s http://localhost:8999/api/health >/dev/null 2>&1; then
    READY=1
    break
  fi
  sleep 0.2
done

if [ "$READY" -ne 1 ]; then
  echo "✗ Failed: Server did not become ready in time"
  exit 1
fi

# 2. Test Empty Body
echo "Testing: Empty Body"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8999/api/hcl/parse -H "Content-Type: application/json" -d '{}')
if [ "$STATUS" -eq 400 ]; then echo "✓ Passed: Empty Body rejected (400)"; else echo "✗ Failed: Expected 400, got $STATUS"; exit 1; fi

# 3. Test Invalid HCL Syntax (Unclosed Braces)
echo "Testing: Unclosed Braces"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8999/api/hcl/parse -H "Content-Type: application/json" -d '{"hcl":"source \"amazon-ebs\" \"vib\" {"}')
if [ "$STATUS" -eq 400 ]; then echo "✓ Passed: Syntax error caught gracefully (400)"; else echo "✗ Failed: Expected 400, got $STATUS"; exit 1; fi

# 4. Test Missing Amazon-EBS Source Block
echo "Testing: Missing required source block"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8999/api/hcl/parse -H "Content-Type: application/json" -d '{"hcl":"build { sources = [\"source.docker.vib\"] }"}')
if [ "$STATUS" -eq 400 ]; then echo "✓ Passed: Missing amazon-ebs block rejected (400)"; else echo "✗ Failed: Expected 400, got $STATUS"; exit 1; fi

# 5. Test Path Traversal in AMI Name
echo "Testing: Path Traversal Input"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8999/api/hcl/generate -H "Content-Type: application/json" -d '{"region":"ap-south-1","instance_type":"t3.micro","source_ami":"ami-0123456789abcdef0","ami_name":"../../etc/passwd"}')
if [ "$STATUS" -eq 400 ]; then echo "✓ Passed: Path traversal rejected (400)"; else echo "✗ Failed: Expected 400, got $STATUS"; exit 1; fi

echo "=== All edge cases handled safely! ==="
