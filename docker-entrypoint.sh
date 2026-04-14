#!/bin/sh
set -e

echo "Waiting for PostgreSQL..."

MAX_RETRIES=30
RETRY=0

while [ $RETRY -lt $MAX_RETRIES ]; do
    if /app/migrate up 2>&1; then
        echo "✅ Migrations applied"
        break
    fi
    RETRY=$((RETRY + 1))
    echo "  Attempt $RETRY/$MAX_RETRIES — DB not ready, retrying in 1s..."
    sleep 1
done

if [ $RETRY -ge $MAX_RETRIES ]; then
    echo "❌ Could not connect to PostgreSQL after $MAX_RETRIES attempts"
    exit 1
fi

echo "Starting IAM Routing server..."
exec /app/server
