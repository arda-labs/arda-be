class RagError(Exception):
    status_code = 400
    code = "rag.error"


class NotFoundError(RagError):
    status_code = 404
    code = "rag.not_found"


class ConflictError(RagError):
    status_code = 409
    code = "rag.conflict"


class PermissionDeniedError(RagError):
    status_code = 403
    code = "rag.forbidden"


class NotSupportedError(RagError):
    status_code = 501
    code = "rag.not_supported_yet"
