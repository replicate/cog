//! Framed codec for worker communication.
//!
//! Uses LengthDelimitedCodec for framing + serde_json for serialization.
//! Works over any AsyncRead/AsyncWrite (pipes, sockets, etc).

use std::io;
use std::marker::PhantomData;

use bytes::{Bytes, BytesMut};
use serde::{de::DeserializeOwned, Serialize};
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
        let json = serde_json::to_vec(&item)
            .map_err(|e| io::Error::new(io::ErrorKind::InvalidData, e))?;
        self.inner.encode(Bytes::from(json), dst)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::protocol::{WorkerRequest, WorkerResponse};

    #[test]
    fn codec_roundtrip_request() {
        let mut codec = JsonCodec::<WorkerRequest>::new();
        let mut buf = BytesMut::new();

        let req = WorkerRequest::Predict {
            id: "test".to_string(),
            input: serde_json::json!({"x": 1}),
        };

        codec.encode(req.clone(), &mut buf).unwrap();
        let decoded = codec.decode(&mut buf).unwrap().unwrap();

        match (req, decoded) {
            (
                WorkerRequest::Predict { id: id1, input: input1 },
                WorkerRequest::Predict { id: id2, input: input2 },
            ) => {
                assert_eq!(id1, id2);
                assert_eq!(input1, input2);
            }
            _ => panic!("mismatch"),
        }
    }

    #[test]
    fn codec_roundtrip_response() {
        let mut codec = JsonCodec::<WorkerResponse>::new();
        let mut buf = BytesMut::new();

        let resp = WorkerResponse::Ready;
        codec.encode(resp, &mut buf).unwrap();
        let decoded = codec.decode(&mut buf).unwrap().unwrap();

        assert!(matches!(decoded, WorkerResponse::Ready));
    }
}
