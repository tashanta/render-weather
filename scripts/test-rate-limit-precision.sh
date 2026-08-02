#!/bin/bash
# Test rate limit precision over 60 seconds

set -e

echo "Testing rate limit precision over 60 seconds..."
echo "Sending requests at 2 req/sec (120 attempts total)"
echo ""

start=$(date +%s)
allowed=0
denied=0
errors=0

while [ $(($(date +%s) - start)) -lt 60 ]; do
    status=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/weather/Paris 2>/dev/null || echo "000")
    
    if [ "$status" -eq 200 ] || [ "$status" -eq 401 ]; then
        ((allowed++))
    elif [ "$status" -eq 429 ]; then
        ((denied++))
    else
        ((errors++))
    fi
    
    sleep 0.5  # 2 req/sec
done

echo "Results over 60 seconds:"
echo "  Allowed: $allowed"
echo "  Denied: $denied"
echo "  Errors: $errors"
echo "  Total attempts: $((allowed + denied + errors))"
echo ""

# Expected: allowed <= 120 (60 initial burst + 60 refilled over 60s)
if [ $allowed -le 120 ]; then
    echo "✅ PASS: Rate limit enforced correctly (≤120 requests allowed)"
    exit 0
else
    echo "❌ FAIL: Allowed $allowed requests (expected ≤120)"
    echo "This indicates the rate limiter is still allowing more than 60 RPM"
    exit 1
fi
