//! Framed codec for worker communication.
//!
//! Uses LengthDelimitedCodec for framing + serde_json for serialization.
//! Works over any AsyncRead/AsyncWrite (pipes, sockets, etc).

use std::io;
use std::marker::PhantomData;

use serde::{Serialize, de::DeserializeOwned};
use tokio_util::bytes::{Bytes, BytesMut};
use tokio_util::codec::{Decoder, Encoder, LengthDelimitedCodec};

/// Codec that frames messages with length prefix and serializes with JSON.
///
/// Wraps LengthDelimitedCodec and adds serde_json serialization.
pub struct JsonCodec<T> {
    inner: LengthDelimitedCodec,
    _phantom: PhantomData<T>,
}

impl<T> Default for JsonCodec<T> {
    fn default() -> Self {
        Self::new()
    }
}

impl<T> JsonCodec<T> {
    pub fn new() -> Self {
        Self {
            inner: LengthDelimitedCodec::builder()
                .length_field_length(4)
                .new_codec(),
            _phantom: PhantomData,
        }
    }
}

impl<T: DeserializeOwned> Decoder for JsonCodec<T> {
    type Item = T;
    type Error = io::Error;

    fn decode(&mut self, src: &mut BytesMut) -> Result<Option<Self::Item>, Self::Error> {
        match self.inner.decode(src)? {
            Some(bytes) => {
                let item = serde_json::from_slice(&bytes)
                    .map_err(|e| io::Error::new(io::ErrorKind::InvalidData, e))?;
                Ok(Some(item))
            }
            None => Ok(None),
        }
    }
}

impl<T: Serialize> Encoder<T> for JsonCodec<T> {
    type Error = io::Error;

    fn encode(&mut self, item: T, dst: &mut BytesMut) -> Result<(), Self::Error> {
        let json =
            serde_json::to_vec(&item).map_err(|e| io::Error::new(io::ErrorKind::InvalidData, e))?;
        let json_len = json.len();
        // SAFETY: These logs must NOT be shipped over IPC (would create feedback loop).
        // WorkerTracingLayer filters out coglet::bridge::codec target to prevent encoding
        // a WorkerLog message from triggering another log that creates another WorkerLog, etc.
        tracing::trace!(json_size_bytes = json_len, "Encoding frame");
        if json_len > 100_000 {
            tracing::info!(
                json_size_bytes = json_len,
                json_size_kb = json_len / 1024,
                "Large frame being encoded"
            );
        }
        self.inner.encode(Bytes::from(json), dst)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::bridge::protocol::{
        ControlRequest, ControlResponse, SlotId, SlotRequest, SlotResponse,
    };

    #[test]
    fn codec_roundtrip_control_request() {
        let mut codec = JsonCodec::<ControlRequest>::new();
        let mut buf = BytesMut::new();

        let slot = SlotId::new();
        let req = ControlRequest::Cancel { slot };
        codec.encode(req, &mut buf).unwrap();
        let decoded = codec.decode(&mut buf).unwrap().unwrap();

        assert!(matches!(decoded, ControlRequest::Cancel { .. }));
    }

    #[test]
    fn codec_roundtrip_control_response() {
        let mut codec = JsonCodec::<ControlResponse>::new();
        let mut buf = BytesMut::new();

        let slots = vec![SlotId::new()];
        let resp = ControlResponse::Ready {
            slots,
            schema: None,
        };
        codec.encode(resp, &mut buf).unwrap();
        let decoded = codec.decode(&mut buf).unwrap().unwrap();

        assert!(matches!(decoded, ControlResponse::Ready { .. }));
    }

    #[test]
    fn codec_roundtrip_slot_request() {
        let mut codec = JsonCodec::<SlotRequest>::new();
        let mut buf = BytesMut::new();

        let req = SlotRequest::Predict {
            id: "test".to_string(),
            input: serde_json::json!({"x": 1}),
        };

        codec.encode(req.clone(), &mut buf).unwrap();
        let decoded = codec.decode(&mut buf).unwrap().unwrap();

        match (req, decoded) {
            (
                SlotRequest::Predict {
                    id: id1,
                    input: input1,
                },
                SlotRequest::Predict {
                    id: id2,
                    input: input2,
                },
            ) => {
                assert_eq!(id1, id2);
                assert_eq!(input1, input2);
            }
        }
    }

    #[test]
    fn codec_roundtrip_slot_response() {
        let mut codec = JsonCodec::<SlotResponse>::new();
        let mut buf = BytesMut::new();

        let resp = SlotResponse::Done {
            id: "test".to_string(),
            output: Some(serde_json::json!("result")),
            predict_time: 1.5,
        };
        codec.encode(resp, &mut buf).unwrap();
        let decoded = codec.decode(&mut buf).unwrap().unwrap();

        match decoded {
            SlotResponse::Done {
                id,
                output,
                predict_time,
            } => {
                assert_eq!(id, "test");
                assert_eq!(output, Some(serde_json::json!("result")));
                assert!((predict_time - 1.5).abs() < 0.001);
            }
            _ => panic!("wrong variant"),
        }
    }
}
