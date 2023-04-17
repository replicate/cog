import json as json_module
from json.decoder import JSONDecodeError
from typing import Any
from typing import Callable
from typing import Dict
from typing import List
from typing import Optional
from typing import Tuple
from typing import Union
from urllib.parse import parse_qsl
from urllib.parse import urlparse

from cog._vendor.requests import PreparedRequest
from cog._vendor.requests.packages.urllib3.util.url import parse_url


def _create_key_val_str(input_dict: Union[Dict[Any, Any], Any]) -> str:
    """
    Returns string of format {'key': val, 'key2': val2}
    Function is called recursively for nested dictionaries

    :param input_dict: dictionary to transform
    :return: (str) reformatted string
    """

    def list_to_str(input_list: List[str]) -> str:
        """
        Convert all list items to string.
        Function is called recursively for nested lists
        """
        converted_list = []
        for item in sorted(input_list, key=lambda x: str(x)):
            if isinstance(item, dict):
                item = _create_key_val_str(item)
            elif isinstance(item, list):
                item = list_to_str(item)

            converted_list.append(str(item))
        list_str = ", ".join(converted_list)
        return "[" + list_str + "]"

    items_list = []
    for key in sorted(input_dict.keys(), key=lambda x: str(x)):
        val = input_dict[key]
        if isinstance(val, dict):
            val = _create_key_val_str(val)
        elif isinstance(val, list):
            val = list_to_str(input_list=val)

        items_list.append("{}: {}".format(key, val))

    key_val_str = "{{{}}}".format(", ".join(items_list))
    return key_val_str


def _filter_dict_recursively(
    dict1: Dict[Any, Any], dict2: Dict[Any, Any]
) -> Dict[Any, Any]:
    filtered_dict = {}
    for k, val in dict1.items():
        if k in dict2:
            if isinstance(val, dict):
                val = _filter_dict_recursively(val, dict2[k])
            filtered_dict[k] = val

    return filtered_dict


def urlencoded_params_matcher(
    params: Optional[Dict[str, str]], *, allow_blank: bool = False
) -> Callable[..., Any]:
    """
    Matches URL encoded data

    :param params: (dict) data provided to 'data' arg of request
    :return: (func) matcher
    """

    def match(request: PreparedRequest) -> Tuple[bool, str]:
        reason = ""
        request_body = request.body
        qsl_body = (
            dict(parse_qsl(request_body, keep_blank_values=allow_blank))  # type: ignore[type-var]
            if request_body
            else {}
        )
        params_dict = params or {}
        valid = params is None if request_body is None else params_dict == qsl_body
        if not valid:
            reason = "request.body doesn't match: {} doesn't match {}".format(
                _create_key_val_str(qsl_body), _create_key_val_str(params_dict)
            )

        return valid, reason

    return match


def json_params_matcher(
    params: Optional[Union[Dict[str, Any], List[Any]]], *, strict_match: bool = True
) -> Callable[..., Any]:
    """Matches JSON encoded data of request body.

    Parameters
    ----------
    params : dict or list
        JSON object provided to 'json' arg of request or a part of it if used in
        conjunction with ``strict_match=False``.
    strict_match : bool, default=True
        Applied only when JSON object is a dictionary.
        If set to ``True``, validates that all keys of JSON object match.
        If set to ``False``, original request may contain additional keys.


    Returns
    -------
    Callable
        Matcher function.

    """

    def match(request: PreparedRequest) -> Tuple[bool, str]:
        reason = ""
        request_body = request.body
        json_params = (params or {}) if not isinstance(params, list) else params
        try:
            if isinstance(request_body, bytes):
                request_body = request_body.decode("utf-8")
            json_body = json_module.loads(request_body) if request_body else {}

            if (
                not strict_match
                and isinstance(json_body, dict)
                and isinstance(json_params, dict)
            ):
                # filter down to just the params specified in the matcher
                json_body = _filter_dict_recursively(json_body, json_params)

            valid = params is None if request_body is None else json_params == json_body

            if not valid:
                if isinstance(json_body, dict) and isinstance(json_params, dict):
                    reason = "request.body doesn't match: {} doesn't match {}".format(
                        _create_key_val_str(json_body), _create_key_val_str(json_params)
                    )
                else:
                    reason = f"request.body doesn't match: {json_body} doesn't match {json_params}"

                if not strict_match:
                    reason += (
                        "\nNote: You use non-strict parameters check, "
                        "to change it use `strict_match=True`."
                    )

        except JSONDecodeError:
            valid = False
            reason = (
                "request.body doesn't match: JSONDecodeError: Cannot parse request.body"
            )

        return valid, reason

    return match


def fragment_identifier_matcher(identifier: Optional[str]) -> Callable[..., Any]:
    def match(request: PreparedRequest) -> Tuple[bool, str]:
        reason = ""
        url_fragment = urlparse(request.url).fragment
        if identifier:
            url_fragment_qsl = sorted(parse_qsl(url_fragment))  # type: ignore[type-var]
            identifier_qsl = sorted(parse_qsl(identifier))
            valid = identifier_qsl == url_fragment_qsl
        else:
            valid = not url_fragment

        if not valid:
            reason = (
                "URL fragment identifier is different: "  # type: ignore[str-bytes-safe]
                f"{identifier} doesn't match {url_fragment}"
            )

        return valid, reason

    return match


def query_param_matcher(
    params: Optional[Dict[str, Any]], *, strict_match: bool = True
) -> Callable[..., Any]:
    """Matcher to match 'params' argument in request.

    Parameters
    ----------
    params : dict
        The same as provided to request or a part of it if used in
        conjunction with ``strict_match=False``.
    strict_match : bool, default=True
        If set to ``True``, validates that all parameters match.
        If set to ``False``, original request may contain additional parameters.


    Returns
    -------
    Callable
        Matcher function.

    """

    params_dict = params or {}

    for k, v in params_dict.items():
        if isinstance(v, (int, float)):
            params_dict[k] = str(v)

    def match(request: PreparedRequest) -> Tuple[bool, str]:
        reason = ""
        request_params = request.params  # type: ignore[attr-defined]
        request_params_dict = request_params or {}

        if not strict_match:
            # filter down to just the params specified in the matcher
            request_params_dict = {
                k: v for k, v in request_params_dict.items() if k in params_dict
            }

        valid = sorted(params_dict.items()) == sorted(request_params_dict.items())

        if not valid:
            reason = "Parameters do not match. {} doesn't match {}".format(
                _create_key_val_str(request_params_dict),
                _create_key_val_str(params_dict),
            )
            if not strict_match:
                reason += (
                    "\nYou can use `strict_match=True` to do a strict parameters check."
                )

        return valid, reason

    return match


def query_string_matcher(query: Optional[str]) -> Callable[..., Any]:
    """
    Matcher to match query string part of request

    :param query: (str), same as constructed by request
    :return: (func) matcher
    """

    def match(request: PreparedRequest) -> Tuple[bool, str]:
        reason = ""
        data = parse_url(request.url)
        request_query = data.query

        request_qsl = sorted(parse_qsl(request_query)) if request_query else {}
        matcher_qsl = sorted(parse_qsl(query)) if query else {}

        valid = not query if request_query is None else request_qsl == matcher_qsl

        if not valid:
            reason = "Query string doesn't match. {} doesn't match {}".format(
                _create_key_val_str(dict(request_qsl)),
                _create_key_val_str(dict(matcher_qsl)),
            )

        return valid, reason

    return match


def request_kwargs_matcher(kwargs: Optional[Dict[str, Any]]) -> Callable[..., Any]:
    """
    Matcher to match keyword arguments provided to request

    :param kwargs: (dict), keyword arguments, same as provided to request
    :return: (func) matcher
    """

    def match(request: PreparedRequest) -> Tuple[bool, str]:
        reason = ""
        kwargs_dict = kwargs or {}
        # validate only kwargs that were requested for comparison, skip defaults
        req_kwargs = request.req_kwargs  # type: ignore[attr-defined]
        request_kwargs = {k: v for k, v in req_kwargs.items() if k in kwargs_dict}

        valid = (
            not kwargs_dict
            if not request_kwargs
            else sorted(kwargs_dict.items()) == sorted(request_kwargs.items())
        )

        if not valid:
            reason = "Arguments don't match: {} doesn't match {}".format(
                _create_key_val_str(request_kwargs), _create_key_val_str(kwargs_dict)
            )

        return valid, reason

    return match


def multipart_matcher(
    files: Dict[str, Any], data: Optional[Dict[str, str]] = None
) -> Callable[..., Any]:
    """
    Matcher to match 'multipart/form-data' content-type.
    This function constructs request body and headers from provided 'data' and 'files'
    arguments and compares to actual request

    :param files: (dict), same as provided to request
    :param data: (dict), same as provided to request
    :return: (func) matcher
    """
    if not files:
        raise TypeError("files argument cannot be empty")

    prepared = PreparedRequest()
    prepared.headers = {"Content-Type": ""}  # type: ignore[assignment]
    prepared.prepare_body(data=data, files=files)

    def get_boundary(content_type: str) -> str:
        """
        Parse 'boundary' value from header.

        :param content_type: (str) headers["Content-Type"] value
        :return: (str) boundary value
        """
        if "boundary=" not in content_type:
            return ""

        return content_type.split("boundary=")[1]

    def match(request: PreparedRequest) -> Tuple[bool, str]:
        reason = "multipart/form-data doesn't match. "
        if "Content-Type" not in request.headers:
            return False, reason + "Request is missing the 'Content-Type' header"

        request_boundary = get_boundary(request.headers["Content-Type"])
        prepared_boundary = get_boundary(prepared.headers["Content-Type"])

        # replace boundary value in header and in body, since by default
        # urllib3.filepost.encode_multipart_formdata dynamically calculates
        # random boundary alphanumeric value
        request_content_type = request.headers["Content-Type"]
        prepared_content_type = prepared.headers["Content-Type"].replace(
            prepared_boundary, request_boundary
        )

        request_body = request.body
        prepared_body = prepared.body or ""

        if isinstance(prepared_body, bytes):
            # since headers always come as str, need to convert to bytes
            prepared_boundary = prepared_boundary.encode("utf-8")  # type: ignore[assignment]
            request_boundary = request_boundary.encode("utf-8")  # type: ignore[assignment]

        prepared_body = prepared_body.replace(
            prepared_boundary, request_boundary  # type: ignore[arg-type]
        )

        headers_valid = prepared_content_type == request_content_type
        if not headers_valid:
            return (
                False,
                reason
                + "Request headers['Content-Type'] is different. {} isn't equal to {}".format(
                    request_content_type, prepared_content_type
                ),
            )

        body_valid = prepared_body == request_body
        if not body_valid:
            return (
                False,
                reason
                + "Request body differs. {} aren't equal {}".format(  # type: ignore[str-bytes-safe]
                    request_body, prepared_body
                ),
            )

        return True, ""

    return match


def header_matcher(
    headers: Dict[str, str], strict_match: bool = False
) -> Callable[..., Any]:
    """
    Matcher to match 'headers' argument in request using the responses library.

    Because ``requests`` will send several standard headers in addition to what
    was specified by your code, request headers that are additional to the ones
    passed to the matcher are ignored by default. You can change this behaviour
    by passing ``strict_match=True``.

    :param headers: (dict), same as provided to request
    :param strict_match: (bool), whether headers in addition to those specified
                         in the matcher should cause the match to fail.
    :return: (func) matcher
    """

    def match(request: PreparedRequest) -> Tuple[bool, str]:
        request_headers: Union[Dict[Any, Any], Any] = request.headers or {}

        if not strict_match:
            # filter down to just the headers specified in the matcher
            request_headers = {k: v for k, v in request_headers.items() if k in headers}

        valid = sorted(headers.items()) == sorted(request_headers.items())

        if not valid:
            return False, "Headers do not match: {} doesn't match {}".format(
                _create_key_val_str(request_headers), _create_key_val_str(headers)
            )

        return valid, ""

    return match
