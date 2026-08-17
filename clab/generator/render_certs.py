import os
import shutil
import subprocess
from pathlib import Path

from .constants import CA_CERT_PATH, CA_KEY_PATH, CERT_LIFETIME_DAYS


def pop_identity(node_name):
    return f"{node_name}.maeto.net"


def cpe_identity(cpe_id, customer_id):
    return f"cpe-{cpe_id}.cust-{customer_id}.cpe.maeto.net"


def verify_ca_cert_exist():
    for path in (CA_CERT_PATH, CA_KEY_PATH):
        if not Path(path).exists():
            print(f"{path} not found. Run `make pki` to generate the CA cert and key.")
            return False

    return True


def copy_ca_cert(out):
    dst = os.path.join(out, "conf", "shared", "ca-cert.pem")
    os.makedirs(os.path.dirname(dst), exist_ok=True)
    shutil.copyfile(CA_CERT_PATH, dst)


def render_cert(out, node_name, identity):
    key_path = os.path.join(out, "conf", node_name, "key.pem")
    cert_path = os.path.join(out, "conf", node_name, "cert.pem")

    if os.path.exists(key_path) and os.path.exists(cert_path):
        return

    os.makedirs(os.path.dirname(key_path), exist_ok=True)

    key = _pki("--gen", "--type", "ecdsa", "--size", "256", "--outform", "pem")
    _write_bytes(key_path, key, 0o600)

    pub = _pki("--pub", "--in", key_path, "--type", "priv")
    cert = _pki(
        "--issue",
        "--lifetime", str(CERT_LIFETIME_DAYS),
        "--cacert", CA_CERT_PATH,
        "--cakey", CA_KEY_PATH,
        "--dn", f"CN={identity}",
        "--san", identity,
        "--outform", "pem",
        stdin=pub,
    )
    _write_bytes(cert_path, cert, 0o644)


def _pki(*args, stdin=None):
    result = subprocess.run(["pki", *args], input=stdin, capture_output=True, check=False)
    if result.returncode != 0 or not result.stdout:
        raise RuntimeError(f"pki {' '.join(args)} failed: {result.stderr.decode().strip()}")

    return result.stdout


def _write_bytes(path, data, mode):
    with open(path, "wb") as f:
        f.write(data)
    os.chmod(path, mode)
