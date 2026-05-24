from app.tools.local_security import LocalSecurityClient, SecurityClientError, SecurityMatch


class SecurityClient(LocalSecurityClient):
    """Backward-compatible name used by the M4 graph."""


__all__ = ["SecurityClient", "SecurityClientError", "SecurityMatch"]
