#!/usr/bin/env sh

# If some custom certificates are provided, add them to the system
if [ -n "$(ls -A /usr/local/share/ca-certificates)" ]; then
  update-ca-certificates
fi

# Start the application
exec "$@"

