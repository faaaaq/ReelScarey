import {
  createContext,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  Disc3,
  Pause,
  Play,
  Repeat,
  Repeat1,
  Shuffle,
  SkipBack,
  SkipForward,
  Volume2,
  VolumeX,
  X,
} from "lucide-react";
import { Link } from "react-router-dom";
import {
  APIError,
  api,
  messageFor,
  type MusicPreferences,
  type MusicStreamingQuality,
} from "./api";
import { useResource } from "./useResource";
import { STREAMING_QUALITY_OPTIONS } from "./musicProfiles";
import "./musicPlayer.css";

/** One playable registered file. The server never exposes its path. */
export type PlayerTrack = {
  fileID: number;
  title: string;
  artist: string;
  album: string;
  coverURL?: string;
  durationMS?: number;
  albumID?: number;
  /** Effective codec/bitrate of the playing file, when the caller knows it. */
  formatLabel?: string;
};

const PLAYER_STORAGE_KEY = "reelscarey:music-player:v1";

export type RepeatMode = "off" | "all" | "one";

type RestoredPlayer = {
  queue: PlayerTrack[];
  index: number;
  position: number;
  duration: number;
  volume: number;
  muted: boolean;
  shuffle: boolean;
  repeat: RepeatMode;
};

/** Restore only bounded, non-secret browser state; media paths never appear. */
function restorePlayer(): RestoredPlayer {
  const empty: RestoredPlayer = {
    queue: [],
    index: -1,
    position: 0,
    duration: 0,
    volume: 0.8,
    muted: false,
    shuffle: false,
    repeat: "off",
  };
  try {
    const raw = window.localStorage.getItem(PLAYER_STORAGE_KEY);
    if (!raw) return empty;
    const value = JSON.parse(raw) as Partial<RestoredPlayer>;
    if (!Array.isArray(value.queue)) return empty;
    const queue = value.queue
      .filter((track): track is PlayerTrack =>
        Boolean(
          track &&
          typeof track === "object" &&
          Number.isInteger(track.fileID) &&
          track.fileID > 0 &&
          typeof track.title === "string" &&
          typeof track.artist === "string" &&
          typeof track.album === "string",
        ),
      )
      .slice(0, 500);
    const index =
      Number.isInteger(value.index) &&
      (value.index as number) >= 0 &&
      (value.index as number) < queue.length
        ? (value.index as number)
        : -1;
    const finite = (candidate: unknown, fallback: number) =>
      typeof candidate === "number" && Number.isFinite(candidate)
        ? candidate
        : fallback;
    return {
      queue,
      index,
      position: Math.max(0, Math.min(finite(value.position, 0), 86400)),
      duration: Math.max(0, Math.min(finite(value.duration, 0), 86400)),
      volume: Math.max(0, Math.min(finite(value.volume, 0.8), 1)),
      muted: value.muted === true,
      shuffle: value.shuffle === true,
      repeat:
        value.repeat === "all" || value.repeat === "one" ? value.repeat : "off",
    };
  } catch {
    return empty;
  }
}

type PlayerValue = {
  current: PlayerTrack | null;
  queue: PlayerTrack[];
  index: number;
  playing: boolean;
  position: number;
  duration: number;
  volume: number;
  muted: boolean;
  shuffle: boolean;
  repeat: RepeatMode;
  error: string;
  errorRecovery: "" | "music-import";
  play: (track: PlayerTrack, queue?: PlayerTrack[]) => void;
  enqueue: (track: PlayerTrack, playNext?: boolean) => void;
  playAt: (index: number) => void;
  removeAt: (index: number) => void;
  moveQueueItem: (from: number, to: number) => void;
  clearQueue: () => void;
  toggleShuffle: () => void;
  cycleRepeat: () => void;
  retry: () => void;
  toggle: () => void;
  next: () => void;
  previous: () => void;
  stop: () => void;
  seek: (seconds: number) => void;
  changeVolume: (value: number) => void;
  toggleMute: () => void;
};

const PlayerContext = createContext<PlayerValue | null>(null);

export function useMusicPlayer() {
  const value = useContext(PlayerContext);
  if (!value)
    throw new Error("useMusicPlayer must be used inside MusicPlayerProvider");
  return value;
}

export function musicStreamURL(fileID: number) {
  return `/api/v1/library/music/files/${fileID}/stream`;
}

/**
 * Capability negotiation for browsers that cannot decode the server's default
 * streaming container (Ogg Opus). canPlayType("") means the browser reports no
 * support, so the player requests an MP3 stream the server converts on the
 * fly; browsers with Opus support keep the server's quality-driven default.
 */
export function browserStreamSuffix(audio: HTMLAudioElement | null) {
  if (!audio || typeof audio.canPlayType !== "function") return "";
  if (audio.canPlayType('audio/ogg; codecs="opus"') === "")
    return "?format=mp3&maxBitRate=192";
  return "";
}

function seekableContains(audio: HTMLAudioElement, seconds: number) {
  for (let index = 0; index < audio.seekable.length; index += 1) {
    if (
      seconds >= audio.seekable.start(index) &&
      seconds <= audio.seekable.end(index)
    )
      return true;
  }
  return false;
}

function clock(seconds: number) {
  if (!Number.isFinite(seconds) || seconds <= 0) return "0:00";
  const whole = Math.floor(seconds);
  return `${Math.floor(whole / 60)}:${String(whole % 60).padStart(2, "0")}`;
}

export function MusicPlayerProvider({ children }: { children: ReactNode }) {
  const audioRef = useRef<HTMLAudioElement>(null);
  // Monotonic counter for play() requests. A superseded load (rapid track
  // skipping) rejects its play() promise after the new track's play() has
  // already resolved; that stale rejection must not flip `playing` back to
  // false while the new track is actually playing.
  const playEpoch = useRef(0);
  // Offset responses contain only the remainder of a track. Keep their media
  // timeline separate from the full-track position shown by the player.
  const streamOffset = useRef(0);
  const [restored] = useState(restorePlayer);
  const [current, setCurrent] = useState<PlayerTrack | null>(() =>
    restored.index >= 0 ? restored.queue[restored.index] : null,
  );
  const [queue, setQueue] = useState<PlayerTrack[]>(restored.queue);
  const [index, setIndex] = useState(restored.index);
  const [playing, setPlaying] = useState(false);
  const [position, setPosition] = useState(restored.position);
  const [duration, setDuration] = useState(restored.duration);
  const [volume, setVolume] = useState(restored.volume);
  const [muted, setMuted] = useState(restored.muted);
  const [shuffle, setShuffle] = useState(restored.shuffle);
  const [repeat, setRepeat] = useState<RepeatMode>(restored.repeat);
  const [error, setErrorState] = useState("");
  const [errorRecovery, setErrorRecovery] = useState<"" | "music-import">("");
  function setError(message: string, recovery: "" | "music-import" = "") {
    setErrorState(message);
    setErrorRecovery(recovery);
  }

  useEffect(() => {
    document.body.classList.toggle("has-music-player", Boolean(current));
    return () => document.body.classList.remove("has-music-player");
  }, [current]);

  // Reload restoration is deliberately browser-local, not cross-device server
  // synchronization. Named Subsonic accounts use their durable server queues.
  useEffect(() => {
    const timer = window.setTimeout(() => {
      try {
        if (!current) {
          window.localStorage.removeItem(PLAYER_STORAGE_KEY);
          return;
        }
        window.localStorage.setItem(
          PLAYER_STORAGE_KEY,
          JSON.stringify({
            queue,
            index,
            position,
            duration,
            volume,
            muted,
            shuffle,
            repeat,
          }),
        );
      } catch {
        // Storage may be unavailable in private/embedded browsing; playback
        // remains functional and simply does not promise restoration.
      }
    }, 250);
    return () => window.clearTimeout(timer);
  }, [
    current,
    queue,
    index,
    position,
    duration,
    volume,
    muted,
    shuffle,
    repeat,
  ]);

  useEffect(() => {
    const clear = () => stop();
    window.addEventListener("reelscarey:unauthorized", clear);
    window.addEventListener("reelscarey:logout", clear);
    return () => {
      window.removeEventListener("reelscarey:unauthorized", clear);
      window.removeEventListener("reelscarey:logout", clear);
    };
  }, []);

  /**
   * Requests playback and tracks the request generation. Setting `src` and
   * calling `load()` already aborts the previous in-flight fetch, so only the
   * stale promise result needs fencing: results from an older epoch are
   * ignored instead of clobbering the current playing state.
   */
  function requestPlay(audio: HTMLAudioElement) {
    const epoch = ++playEpoch.current;
    void audio
      .play()
      .then(() => {
        if (epoch === playEpoch.current) setPlaying(true);
      })
      .catch((e: unknown) => {
        if (epoch !== playEpoch.current) return;
        setPlaying(false);
        // Autoplay policies reject programmatic play before any request is
        // made; that is not a server failure and must be phrased as one the
        // user can act on (RS-0008).
        if (e instanceof DOMException && e.name === "NotAllowedError")
          setError("The browser blocked autoplay. Press play to start.");
      });
  }

  function start(track: PlayerTrack, offset = 0) {
    const audio = audioRef.current;
    if (!audio) return;
    const resumeAt = Math.max(0, offset);
    setCurrent(track);
    streamOffset.current = resumeAt;
    setPosition(resumeAt);
    setDuration((current) =>
      resumeAt > 0 && current > resumeAt
        ? current
        : track.durationMS
          ? track.durationMS / 1000
          : 0,
    );
    setError("");
    const source = new URL(
      musicStreamURL(track.fileID) + browserStreamSuffix(audio),
      window.location.origin,
    );
    if (resumeAt > 0)
      source.searchParams.set("timeOffset", String(Math.floor(resumeAt)));
    audio.src = `${source.pathname}${source.search}`;
    audio.load();
    requestPlay(audio);
  }

  function play(track: PlayerTrack, requestedQueue?: PlayerTrack[]) {
    if (!track.fileID) return;
    const list = (requestedQueue ?? [])
      .filter((item) => item.fileID > 0)
      .filter(
        (item, itemIndex, all) =>
          all.findIndex((other) => other.fileID === item.fileID) === itemIndex,
      );
    let nextQueue = list.length ? list : [track];
    let nextIndex = nextQueue.findIndex((item) => item.fileID === track.fileID);
    if (nextIndex < 0) {
      nextQueue = [track, ...nextQueue];
      nextIndex = 0;
    }
    setQueue(nextQueue);
    setIndex(nextIndex);
    if (current?.fileID === track.fileID && audioRef.current) {
      if (!audioRef.current.getAttribute("src")) {
        start(track);
        return;
      }
      if (streamOffset.current > 0) {
        start(track);
        return;
      }
      setPosition(0);
      audioRef.current.currentTime = 0;
      setError("");
      requestPlay(audioRef.current);
      return;
    }
    start(track);
  }

  function enqueue(track: PlayerTrack, playNext = false) {
    if (!Number.isInteger(track.fileID) || track.fileID <= 0) return;
    setQueue((currentQueue) => {
      if (currentQueue.some((item) => item.fileID === track.fileID))
        return currentQueue;
      const nextQueue = [...currentQueue];
      const insertion = playNext
        ? index >= 0
          ? Math.min(index + 1, nextQueue.length)
          : 0
        : nextQueue.length;
      nextQueue.splice(insertion, 0, track);
      return nextQueue;
    });
  }

  function playAt(requestedIndex: number) {
    if (
      !Number.isInteger(requestedIndex) ||
      requestedIndex < 0 ||
      requestedIndex >= queue.length
    )
      return;
    setIndex(requestedIndex);
    start(queue[requestedIndex]);
  }

  function selectPaused(track: PlayerTrack) {
    const audio = audioRef.current;
    // Invalidate a pending play() before unloading the removed selection.
    playEpoch.current += 1;
    audio?.pause();
    if (audio?.getAttribute("src")) {
      audio.removeAttribute("src");
      audio.load();
    }
    streamOffset.current = 0;
    setCurrent(track);
    setPlaying(false);
    setPosition(0);
    setDuration(track.durationMS ? track.durationMS / 1000 : 0);
    setError("");
  }

  function removeAt(requestedIndex: number) {
    if (
      !Number.isInteger(requestedIndex) ||
      requestedIndex < 0 ||
      requestedIndex >= queue.length
    )
      return;
    const nextQueue = queue.filter(
      (_, itemIndex) => itemIndex !== requestedIndex,
    );
    if (requestedIndex !== index) {
      setQueue(nextQueue);
      if (index > requestedIndex) setIndex(index - 1);
      return;
    }
    if (!nextQueue.length) {
      clearQueue();
      return;
    }
    // The entry at the same index is the next survivor; at the end, the last
    // survivor is the previous entry. Keep playback intent across the swap.
    const nextIndex = Math.min(requestedIndex, nextQueue.length - 1);
    setQueue(nextQueue);
    setIndex(nextIndex);
    if (playing) start(nextQueue[nextIndex]);
    else selectPaused(nextQueue[nextIndex]);
  }

  function moveQueueItem(from: number, to: number) {
    if (
      !Number.isInteger(from) ||
      !Number.isInteger(to) ||
      from < 0 ||
      to < 0 ||
      from >= queue.length ||
      to >= queue.length ||
      from === to
    )
      return;
    const nextQueue = [...queue];
    const [moved] = nextQueue.splice(from, 1);
    nextQueue.splice(to, 0, moved);
    let nextIndex = index;
    if (index === from) nextIndex = to;
    else if (from < index && to >= index) nextIndex = index - 1;
    else if (from > index && to <= index) nextIndex = index + 1;
    setQueue(nextQueue);
    setIndex(nextIndex);
  }

  function toggleShuffle() {
    setShuffle((value) => !value);
  }

  function cycleRepeat() {
    setRepeat((value) =>
      value === "off" ? "all" : value === "all" ? "one" : "off",
    );
  }

  function retry() {
    if (current) start(current, position);
  }

  function toggle() {
    const audio = audioRef.current;
    if (!audio || !current) return;
    if (audio.paused) {
      setError("");
      if (!audio.getAttribute("src")) {
        start(current, position);
        return;
      }
      requestPlay(audio);
    } else {
      audio.pause();
    }
  }

  function finishPlayback() {
    audioRef.current?.pause();
    setPlaying(false);
    setPosition(0);
  }

  function advance(automatic: boolean) {
    // Repeat-one belongs to the media element's ended event only. A manual
    // Next always advances, including when only one track is queued.
    if (automatic && repeat === "one" && current) {
      start(current);
      return;
    }
    if (index < 0 || !queue.length) {
      finishPlayback();
      return;
    }
    if (shuffle && queue.length > 1) {
      // Pick from all indexes except the current one without retry loops.
      const candidate = Math.max(
        0,
        Math.min(
          queue.length - 2,
          Math.floor(Math.random() * (queue.length - 1)),
        ),
      );
      const nextIndex = candidate >= index ? candidate + 1 : candidate;
      setIndex(nextIndex);
      start(queue[nextIndex]);
      return;
    }
    const nextIndex = index + 1;
    if (nextIndex < queue.length) {
      setIndex(nextIndex);
      start(queue[nextIndex]);
      return;
    }
    if (repeat === "all") {
      setIndex(0);
      start(queue[0]);
      return;
    }
    finishPlayback();
  }

  function next() {
    advance(false);
  }

  function previous() {
    const audio = audioRef.current;
    if (!audio) return;
    if (streamOffset.current + audio.currentTime > 3) {
      if (current && streamOffset.current > 0) start(current);
      else {
        streamOffset.current = 0;
        audio.currentTime = 0;
        setPosition(0);
      }
      return;
    }
    if (index > 0) {
      const previousIndex = index - 1;
      setIndex(previousIndex);
      start(queue[previousIndex]);
      return;
    }
    if (current && streamOffset.current > 0) start(current);
    else {
      streamOffset.current = 0;
      audio.currentTime = 0;
      setPosition(0);
    }
  }

  function clearQueue() {
    const audio = audioRef.current;
    // A late play() resolution from the cleared selection must not resurrect
    // the playing state after the audio element has been unloaded.
    playEpoch.current += 1;
    if (audio?.getAttribute("src")) {
      audio.pause();
      audio.removeAttribute("src");
      audio.load();
    }
    try {
      window.localStorage.removeItem(PLAYER_STORAGE_KEY);
    } catch {
      // Playback teardown must not fail when browser storage is unavailable.
    }
    streamOffset.current = 0;
    setCurrent(null);
    setQueue([]);
    setIndex(-1);
    setPlaying(false);
    setPosition(0);
    setDuration(0);
    setError("");
  }

  // Retain the original public action for existing player consumers.
  function stop() {
    clearQueue();
  }

  function seek(seconds: number) {
    const audio = audioRef.current;
    if (!audio || !current) return;
    const total =
      duration || (current.durationMS ? current.durationMS / 1000 : 0);
    const value = Math.max(0, Math.min(seconds, total || seconds));
    const relative = value - streamOffset.current;
    if (relative >= 0 && seekableContains(audio, relative)) {
      // File-backed direct/cache responses expose normal browser ranges. The
      // element time is relative to any offset response already loaded.
      audio.currentTime = relative;
      setPosition(value);
      return;
    }

    // A live FFmpeg pipe cannot satisfy an unbuffered byte-range seek. Reload
    // from a media-time offset instead of pretending currentTime succeeded.
    // Preserve capability/format parameters already chosen for this element.
    const source = new URL(
      audio.src || musicStreamURL(current.fileID),
      window.location.origin,
    );
    source.searchParams.set("timeOffset", String(Math.floor(value)));
    const resume = !audio.paused;
    streamOffset.current = value;
    audio.src = `${source.pathname}${source.search}`;
    audio.load();
    setPosition(value);
    setError("");
    if (resume) requestPlay(audio);
  }

  function changeVolume(value: number) {
    setVolume(Math.max(0, Math.min(1, value)));
    if (value > 0) setMuted(false);
  }

  function toggleMute() {
    setMuted((value) => !value);
  }

  useEffect(() => {
    const audio = audioRef.current;
    if (!audio) return;
    audio.volume = volume;
    audio.muted = muted;
  }, [volume, muted]);

  useEffect(() => {
    const audio = audioRef.current;
    if (!audio) return;
    const updatePosition = () =>
      setPosition(streamOffset.current + (audio.currentTime || 0));
    const updateDuration = () => {
      if (Number.isFinite(audio.duration) && audio.duration > 0) {
        setDuration(streamOffset.current + audio.duration);
      }
    };
    const onPlay = () => setPlaying(true);
    const onPause = () => setPlaying(false);
    const onEnded = () => advance(true);
    const onError = () => {
      setPlaying(false);
      // The media element only reports "something failed"; probe the stream
      // endpoint once so auth expiry, permissions and missing files get
      // distinct, actionable messages and an expired session reaches the
      // same login funnel as every other page (RS-0008).
      const source = audio.currentSrc || audio.src;
      setError("Playback is unavailable for this file.");
      if (!source) return;
      void fetch(source, { headers: { Range: "bytes=0-0" } }).then(
        async (response) => {
          let code = "";
          if (!response.ok) {
            try {
              const body = (await response.json()) as {
                error?: { code?: string };
              };
              code = body.error?.code ?? "";
            } catch {
              // Some proxies replace the structured API error with plain text.
            }
          }
          if (response.status === 401) {
            window.dispatchEvent(new Event("reelscarey:unauthorized"));
            setError("Sign in again to keep listening.");
          } else if (response.status === 403) {
            setError("This account cannot play music files.");
          } else if (response.status === 404) {
            if (code === "music_file_missing") {
              setError(
                "The registered file is missing or unreadable on the server. Restore it or import a replacement, then retry.",
                "music-import",
              );
            } else {
              setError(
                "This file is no longer registered in the music library.",
              );
            }
          } else if (!response.ok) {
            setError("Playback is unavailable for this file.");
          }
        },
        () => {
          setError("Playback stopped because ReelScarey could not be reached.");
        },
      );
    };
    audio.addEventListener("timeupdate", updatePosition);
    audio.addEventListener("durationchange", updateDuration);
    audio.addEventListener("loadedmetadata", updateDuration);
    audio.addEventListener("play", onPlay);
    audio.addEventListener("pause", onPause);
    audio.addEventListener("ended", onEnded);
    audio.addEventListener("error", onError);
    return () => {
      audio.removeEventListener("timeupdate", updatePosition);
      audio.removeEventListener("durationchange", updateDuration);
      audio.removeEventListener("loadedmetadata", updateDuration);
      audio.removeEventListener("play", onPlay);
      audio.removeEventListener("pause", onPause);
      audio.removeEventListener("ended", onEnded);
      audio.removeEventListener("error", onError);
    };
  }, [current, queue, index, shuffle, repeat]);

  const value: PlayerValue = {
    current,
    queue,
    index,
    playing,
    position,
    duration: duration || (current?.durationMS ? current.durationMS / 1000 : 0),
    volume,
    muted,
    shuffle,
    repeat,
    error,
    errorRecovery,
    play,
    enqueue,
    playAt,
    removeAt,
    moveQueueItem,
    clearQueue,
    toggleShuffle,
    cycleRepeat,
    retry,
    toggle,
    next,
    previous,
    stop,
    seek,
    changeVolume,
    toggleMute,
  };

  return (
    <PlayerContext.Provider value={value}>
      {children}
      <audio ref={audioRef} preload="metadata" className="music-player-audio" />
    </PlayerContext.Provider>
  );
}

/**
 * Streaming quality selector. The backend reads `streaming_quality` from the
 * music preferences when transcoding a stream, so the selector writes the
 * preference; it does not change the audio element URL itself.
 */
function PlayerQualityControl() {
  const track = useMusicPlayer().current;
  const prefs = useResource<MusicPreferences>("/library/music/preferences");
  const [value, setValue] = useState<MusicStreamingQuality>("balanced");
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");

  useEffect(() => {
    if (prefs.data) setValue(prefs.data.streaming_quality);
  }, [prefs.data]);

  async function change(next: MusicStreamingQuality) {
    if (!prefs.data || saving) {
      // Never show a selection that was not attempted: while a save is in
      // flight the select is disabled, and without loaded preferences there
      // is no revision to fence the save with.
      if (prefs.data) setValue(prefs.data.streaming_quality);
      return;
    }
    setValue(next);
    setSaving(true);
    setSaveError("");
    try {
      await api("/library/music/preferences", {
        method: "PUT",
        body: JSON.stringify({ ...prefs.data, streaming_quality: next }),
      });
      prefs.reload();
    } catch (e) {
      if (
        e instanceof APIError &&
        (e.status === 409 || e.code === "revision_conflict")
      ) {
        prefs.reload();
        setSaveError(
          "The streaming quality changed elsewhere. The saved setting was reloaded.",
        );
      } else {
        setSaveError(messageFor(e));
      }
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="music-player-quality">
      <select
        aria-label="Streaming quality"
        value={value}
        disabled={saving || !prefs.data}
        onChange={(event) =>
          void change(event.target.value as MusicStreamingQuality)
        }
      >
        {!prefs.data && <option value="">Loading…</option>}
        {STREAMING_QUALITY_OPTIONS.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
      {track?.formatLabel && (
        <span className="music-player-meta">{track.formatLabel}</span>
      )}
      {saveError && (
        <span className="music-player-error" role="status">
          {saveError}
        </span>
      )}
    </div>
  );
}

export function MusicPlayerBar() {
  const player = useMusicPlayer();
  const track = player.current;
  const [coverFailed, setCoverFailed] = useState(false);
  useEffect(() => setCoverFailed(false), [track?.coverURL]);
  if (!track) return null;
  const total = Math.max(0, player.duration);
  const position = Math.min(Math.max(0, player.position), total || 0);
  return (
    <section className="music-player" aria-label="Music player">
      <div className="music-player-track">
        {track.coverURL && !coverFailed ? (
          <img
            className="music-player-cover"
            src={track.coverURL}
            alt=""
            referrerPolicy="no-referrer"
            onError={() => setCoverFailed(true)}
          />
        ) : (
          <span className="music-player-cover is-empty" aria-hidden="true">
            <Disc3 size={22} strokeWidth={1} />
          </span>
        )}
        <div className="music-player-copy">
          {track.albumID ? (
            <Link
              className="music-player-title"
              to={`/music/albums/${track.albumID}`}
            >
              {track.title}
            </Link>
          ) : (
            <span className="music-player-title">{track.title}</span>
          )}
          <span className="music-player-meta">
            {[track.artist, track.album].filter(Boolean).join(" · ")}
          </span>
          {player.error && (
            <span className="music-player-error" role="status">
              {player.error}
            </span>
          )}
          {player.error && (
            <span className="music-player-error-actions">
              <button type="button" onClick={player.retry}>
                Retry playback
              </button>
              {player.errorRecovery === "music-import" && (
                <Link to="/music/import">Open Music Import</Link>
              )}
            </span>
          )}
        </div>
      </div>
      <div className="music-player-center">
        <div className="music-player-buttons">
          <button
            type="button"
            className={`music-player-button${player.shuffle ? " is-active" : ""}`}
            aria-label="Shuffle"
            aria-pressed={player.shuffle}
            title="Shuffle"
            onClick={player.toggleShuffle}
          >
            <Shuffle size={17} aria-hidden="true" />
          </button>
          <button
            type="button"
            className="music-player-button"
            aria-label="Previous track"
            onClick={player.previous}
          >
            <SkipBack size={18} aria-hidden="true" />
          </button>
          <button
            type="button"
            className="music-player-button is-primary"
            aria-label={player.playing ? "Pause" : "Play"}
            onClick={player.toggle}
          >
            {player.playing ? (
              <Pause size={20} aria-hidden="true" />
            ) : (
              <Play size={20} aria-hidden="true" />
            )}
          </button>
          <button
            type="button"
            className="music-player-button"
            aria-label="Next track"
            onClick={player.next}
          >
            <SkipForward size={18} aria-hidden="true" />
          </button>
          <button
            type="button"
            className={`music-player-button${player.repeat !== "off" ? " is-active" : ""}`}
            aria-label="Repeat"
            aria-pressed={player.repeat !== "off"}
            data-repeat={player.repeat}
            title={`Repeat: ${player.repeat}`}
            onClick={player.cycleRepeat}
          >
            {player.repeat === "one" ? (
              <Repeat1 size={17} aria-hidden="true" />
            ) : (
              <Repeat size={17} aria-hidden="true" />
            )}
          </button>
        </div>
        <div className="music-player-seek">
          <span>{clock(position)}</span>
          <input
            type="range"
            aria-label="Seek"
            aria-disabled={!total}
            aria-valuetext={`${clock(position)} of ${clock(total)}`}
            min={0}
            max={total || 1}
            step={0.1}
            value={position}
            tabIndex={total ? 0 : -1}
            onChange={(event) => player.seek(Number(event.target.value))}
          />
          <span>{clock(total)}</span>
        </div>
      </div>
      <div className="music-player-volume">
        <Link className="music-player-open-full" to="/music/player">
          Open full player
        </Link>
        <PlayerQualityControl />
        <button
          type="button"
          className="music-player-button"
          aria-label={player.muted ? "Unmute" : "Mute"}
          onClick={player.toggleMute}
        >
          {player.muted || player.volume === 0 ? (
            <VolumeX size={18} aria-hidden="true" />
          ) : (
            <Volume2 size={18} aria-hidden="true" />
          )}
        </button>
        <input
          type="range"
          aria-label="Volume"
          min={0}
          max={1}
          step={0.01}
          value={player.muted ? 0 : player.volume}
          onChange={(event) => player.changeVolume(Number(event.target.value))}
        />
        <button
          type="button"
          className="music-player-button"
          aria-label="Close player"
          onClick={player.stop}
        >
          <X size={18} aria-hidden="true" />
        </button>
      </div>
    </section>
  );
}
