"""Generate ignored local-only demo secrets without overwriting existing configuration."""
import getpass
from pathlib import Path
import secrets

path = Path('.env')
if path.exists():
    raise SystemExit('.env already exists; edit it explicitly instead of overwriting it')
password = getpass.getpass('Choose the Relay operator password (at least 12 characters): ')
if len(password) < 12:
    raise SystemExit('Password too short')
path.write_text(f'POSTGRES_PASSWORD={secrets.token_hex(24)}\nRELAY_PASSWORD={password}\n'
                f'RELAY_API_TOKEN={secrets.token_hex(32)}\nCOOKIE_SECURE=0\n')
path.chmod(0o600)
print('Created .env. Operator password is yours; the database and SDK secrets are generated locally.')
