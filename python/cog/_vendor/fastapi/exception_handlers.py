from cog._vendor.fastapi.encoders import jsonable_encoder
from cog._vendor.fastapi.exceptions import RequestValidationError
from cog._vendor.fastapi.utils import is_body_allowed_for_status_code
from cog._vendor.starlette.exceptions import HTTPException
from cog._vendor.starlette.requests import Request
from cog._vendor.starlette.responses import JSONResponse, Response
from cog._vendor.starlette.status import HTTP_422_UNPROCESSABLE_ENTITY


async def http_exception_handler(request: Request, exc: HTTPException) -> Response:
    headers = getattr(exc, "headers", None)
    if not is_body_allowed_for_status_code(exc.status_code):
        return Response(status_code=exc.status_code, headers=headers)
    return JSONResponse(
        {"detail": exc.detail}, status_code=exc.status_code, headers=headers
    )


async def request_validation_exception_handler(
    request: Request, exc: RequestValidationError
) -> JSONResponse:
    return JSONResponse(
        status_code=HTTP_422_UNPROCESSABLE_ENTITY,
        content={"detail": jsonable_encoder(exc.errors())},
    )
