from unittest import mock

import pytest

from cog.director.prediction_tracker import PredictionTracker, PredictionMismatchError
from cog.schema import PredictionResponse


class NotNoneMatcher:
    def __eq__(self, other):
        return other is not None

    def __repr__(self):
        return "<not None>"


NotNone = NotNoneMatcher()


def test_prediction_tracker_basic(match):
    response = PredictionResponse(id="abc123", input={"prompt": "hello, world"})
    webhook_caller = mock.Mock()
    pt = PredictionTracker(response, webhook_caller=webhook_caller)
    pt.start()

    payload = response.copy(update={"logs": "running prediction"})
    pt.update_from_webhook_payload(payload)

    webhook_caller.assert_called_once_with(
        match(
            {
                "logs": "running prediction",
                "started_at": NotNone,
                "completed_at": None,
            }
        )
    )


def test_prediction_tracker_adjusts_status_for_cancelations(match):
    response = PredictionResponse(id="abc123", input={"prompt": "hello, world"})
    webhook_caller = mock.Mock()
    pt = PredictionTracker(response, webhook_caller=webhook_caller)
    pt.start()

    pt.timed_out()
    payload = response.copy(update={"status": "canceled"})
    pt.update_from_webhook_payload(payload)

    webhook_caller.assert_called_once_with(
        match(
            {
                "status": "failed",
                "error": "Prediction timed out",
            }
        )
    )


def test_prediction_tracker_is_complete():
    response = PredictionResponse(id="abc123", input={"prompt": "hello, world"})
    pt = PredictionTracker(response)
    pt.start()

    assert not pt.is_complete()

    response = PredictionResponse(
        id="abc123", input={"prompt": "hello, world"}, status="succeeded"
    )
    pt = PredictionTracker(response)

    assert pt.is_complete()


def test_prediction_tracker_completion_timestamps(match):
    response = PredictionResponse(id="abc123", input={"prompt": "hello, world"})
    webhook_caller = mock.Mock()
    pt = PredictionTracker(response, webhook_caller=webhook_caller)
    pt.start()

    payload = response.copy(update={"status": "succeeded"})
    pt.update_from_webhook_payload(payload)

    webhook_caller.assert_called_once_with(
        match(
            {
                "started_at": NotNone,
                "completed_at": NotNone,
                "metrics": {
                    "predict_time": NotNone,
                },
            }
        )
    )


def test_prediction_tracker_fail(match):
    response = PredictionResponse(id="abc123", input={"prompt": "hello, world"})
    webhook_caller = mock.Mock()
    pt = PredictionTracker(response, webhook_caller=webhook_caller)
    pt.start()

    pt.fail("something went wrong")

    webhook_caller.assert_called_once_with(
        match(
            {
                "status": "failed",
                "error": "something went wrong",
                "started_at": NotNone,
                "completed_at": NotNone,
            }
        )
    )


def test_prediction_tracker_wrong_id():
    response = PredictionResponse(id="abc123", input={"prompt": "hello, world"})
    webhook_caller = mock.Mock()
    pt = PredictionTracker(response, webhook_caller=webhook_caller)
    pt.start()

    payload = PredictionResponse(id="abc456", input={"prompt": "hello, world"})

    with pytest.raises(PredictionMismatchError):
        pt.update_from_webhook_payload(payload)
