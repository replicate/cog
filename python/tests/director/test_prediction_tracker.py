from unittest import mock

import pytest

from cog.director.prediction_tracker import PredictionTracker, PredictionMismatchError
from cog.schema import PredictionResponse


def test_prediction_tracker_basic():
    response = PredictionResponse(id="abc123", input={"prompt": "hello, world"})
    webhook_caller = mock.Mock()
    pt = PredictionTracker(response, webhook_caller=webhook_caller)

    payload = response.copy(update={"logs": "running prediction"})
    pt.update_from_webhook_payload(payload)

    webhook_caller.assert_called_once()
    (webhook_payload,) = webhook_caller.call_args.args
    assert webhook_payload["logs"] == "running prediction"
    assert webhook_payload["started_at"] is not None
    assert webhook_payload["completed_at"] is None


def test_prediction_tracker_adjusts_status_for_cancelations():
    response = PredictionResponse(id="abc123", input={"prompt": "hello, world"})
    webhook_caller = mock.Mock()
    pt = PredictionTracker(response, webhook_caller=webhook_caller)

    pt.timed_out()
    payload = response.copy(update={"status": "canceled"})
    pt.update_from_webhook_payload(payload)

    webhook_caller.assert_called_once()
    (webhook_payload,) = webhook_caller.call_args.args
    assert webhook_payload["status"] == "failed"
    assert webhook_payload["error"] == "Prediction timed out"


def test_prediction_tracker_is_complete():
    response = PredictionResponse(id="abc123", input={"prompt": "hello, world"})
    pt = PredictionTracker(response)

    assert not pt.is_complete()

    response = PredictionResponse(
        id="abc123", input={"prompt": "hello, world"}, status="succeeded"
    )
    pt = PredictionTracker(response)

    assert pt.is_complete()


def test_prediction_tracker_completion_timestamps():
    response = PredictionResponse(id="abc123", input={"prompt": "hello, world"})
    webhook_caller = mock.Mock()
    pt = PredictionTracker(response, webhook_caller=webhook_caller)

    payload = response.copy(update={"status": "succeeded"})
    pt.update_from_webhook_payload(payload)

    webhook_caller.assert_called_once()
    (webhook_payload,) = webhook_caller.call_args.args
    assert webhook_payload["started_at"] is not None
    assert webhook_payload["completed_at"] is not None
    assert webhook_payload["metrics"]["predict_time"] is not None


def test_prediction_tracker_fail():
    response = PredictionResponse(id="abc123", input={"prompt": "hello, world"})
    webhook_caller = mock.Mock()
    pt = PredictionTracker(response, webhook_caller=webhook_caller)

    pt.fail("something went wrong")

    webhook_caller.assert_called_once()
    (webhook_payload,) = webhook_caller.call_args.args
    assert webhook_payload["status"] == "failed"
    assert webhook_payload["error"] == "something went wrong"
    assert webhook_payload["started_at"] is not None
    assert webhook_payload["completed_at"] is not None


def test_prediction_tracker_wrong_id():
    response = PredictionResponse(id="abc123", input={"prompt": "hello, world"})
    webhook_caller = mock.Mock()
    pt = PredictionTracker(response, webhook_caller=webhook_caller)

    payload = PredictionResponse(id="abc456", input={"prompt": "hello, world"})

    with pytest.raises(PredictionMismatchError):
        pt.update_from_webhook_payload(payload)
