import { el } from "./dom.js";

// Recognized media extensions for plain URLs (data: URIs carry their own MIME).
const IMAGE_EXT = /\.(?:png|jpe?g|gif|webp|avif|bmp|svg)(?:[?#]|$)/i;
const AUDIO_EXT = /\.(?:mp3|wav|ogg|oga|flac|m4a|aac|opus)(?:[?#]|$)/i;
const VIDEO_EXT = /\.(?:mp4|webm|ogv|mov|m4v)(?:[?#]|$)/i;

// mediaNode returns an <img>/<audio>/<video> element when `value` is a media
// data: URI or a URL with a recognized media extension, otherwise null. Used
// for both input previews and output rendering so the two stay consistent.
export function mediaNode(value) {
  if (typeof value !== "string" || value === "") return null;
  const isData = value.startsWith("data:");
  if (value.startsWith("data:image/") || (!isData && IMAGE_EXT.test(value))) {
    return el("img", { src: value });
  }
  if (value.startsWith("data:audio/") || (!isData && AUDIO_EXT.test(value))) {
    return el("audio", { controls: true, src: value });
  }
  if (value.startsWith("data:video/") || (!isData && VIDEO_EXT.test(value))) {
    return el("video", { controls: true, src: value });
  }
  return null;
}
