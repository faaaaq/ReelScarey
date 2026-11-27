import {
  createContext,
  useContext,
  useEffect,
  useMemo,
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
  /** Codec/bitrate of the registered file, when the caller knows it. */
  formatLabel?: string;
};

type MusicStreamInfo = {
  codec: string;
  bitrate_kbps?: number;
  mode: string;
  range_seekable: boolean;
};

type MusicStreamStatus =
  | { phase: "idle" | "loading" | "unavailable" }
  | { phase: "ready"; info: MusicStreamInfo };

type StreamRequest = { fileID: number; id: string; checking: boolean };
type QualityResume = { position: number; play: boolean };

const PLAYER_STORAGE_KEY = "reelscarey:music-player:v1";
const PLAYER_PROGRESS_STORAGE_KEY = "reelscarey:music-player-progress:v1";
function playerStorageKeys(identity?: number) {
  const suffix = identity === undefined ? "" : `:central-account:${identity}`;
  return {
    queue: PLAYER_STORAGE_KEY + suffix,
    progress: PLAYER_PROGRESS_STORAGE_KEY + suffix,
  };
}
const PERSISTED_QUEUE_LIMIT = 500;
const PROGRESS_WRITE_INTERVAL = 250;

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

function finite(candidate: unknown, fallback: number) {
  return typeof candidate === "number" && Number.isFinite(candidate)
    ? candidate
    : fallback;
}

/** Restore only bounded, non-secret browser state; media paths never appear. */
function restorePlayer(storageKeys: {
  queue: string;
  progress: string;
}): RestoredPlayer {
  let restored: RestoredPlayer = {
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
    const raw = window.localStorage.getItem(storageKeys.queue);
    if (raw) {
      const value = JSON.parse(raw) as Partial<RestoredPlayer>;
      if (Array.isArray(value.queue)) {
        // Older releases could leave more than 500 entries in storage. Find a
        // valid current item before bounding the restored window so a current
        // track beyond the old cutoff is not silently discarded.
        const completeQueue = value.queue.filter(
          (track): track is PlayerTrack =>
            Boolean(
              track &&
              typeof track === "object" &&
              Number.isInteger(track.fileID) &&
              track.fileID > 0 &&
              typeof track.title === "string" &&
              typeof track.artist === "string" &&
              typeof track.album === "string",
            ),
        );
        const restoredIndex =
          Number.isInteger(value.index) &&
          (value.index as number) >= 0 &&
          (value.index as number) < completeQueue.length
            ? (value.index as number)
            : -1;
        const windowed =
          completeQueue.length > PERSISTED_QUEUE_LIMIT && restoredIndex >= 0;
        const queue = windowed
          ? completeQueue.slice(
              restoredIndex,
              restoredIndex + PERSISTED_QUEUE_LIMIT,
            )
          : completeQueue.slice(0, PERSISTED_QUEUE_LIMIT);
        restored = {
          queue,
          index: windowed ? 0 : restoredIndex,
          position: Math.max(0, Math.min(finite(value.position, 0), 86400)),
          duration: Math.max(0, Math.min(finite(value.duration, 0), 86400)),
          volume: Math.max(0, Math.min(finite(value.volume, 0.8), 1)),
          muted: value.muted === true,
          shuffle: value.shuffle === true,
          repeat:
            value.repeat === "all" || value.repeat === "one"
              ? value.repeat
              : "off",
        };
      }
    }
  } catch {
    // A malformed structural record must not prevent progress/preferences
    // from being restored independently below.
  }

  try {
    const raw = window.localStorage.getItem(storageKeys.progress);
    if (!raw) return restored;
    const value = JSON.parse(raw) as Partial<
      Pick<RestoredPlayer, "position" | "duration" | "volume" | "muted">
    > & { fileID?: number };
    const currentFileID =
      restored.index >= 0 ? restored.queue[restored.index]?.fileID : undefined;
    const matchingTrack =
      currentFileID !== undefined && value.fileID === currentFileID;
    return {
      ...restored,
      // Progress belongs to one file. A stale/interleaved record may still
      // restore global preferences, but never another track's timeline.
      position: matchingTrack
        ? Math.max(
            0,
            Math.min(finite(value.position, restored.position), 86400),
          )
        : restored.position,
      duration: matchingTrack
        ? Math.max(
            0,
            Math.min(finite(value.duration, restored.duration), 86400),
          )
        : restored.duration,
      volume: Math.max(0, Math.min(finite(value.volume, restored.volume), 1)),
      muted: typeof value.muted === "boolean" ? value.muted : restored.muted,
    };
  } catch {
    return restored;
  }
}

export type PlayerValue = {
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
  canManageSettings?: boolean;
  stream: MusicStreamStatus;
  play: (track: PlayerTrack, queue?: PlayerTrack[]) => void;
  playShuffled: (items: PlayerTrack[]) => void;
  enqueue: (track: PlayerTrack, playNext?: boolean) => void;
  playAt: (index: number) => void;
  removeAt: (index: number) => void;
  moveQueueItem: (from: number, to: number) => void;
  clearQueue: () => void;
  toggleShuffle: () => void;
  cycleRepeat: () => void;
  retry: () => void;
  reloadStreamQuality: () => void;
  toggle: () => void;
  next: () => void;
  previous: () => void;
  stop: () => void;
  seek: (seconds: number) => void;
  changeVolume: (value: number) => void;
  toggleMute: () => void;
};

export type MusicQueueValue = Omit<
  PlayerValue,
  "position" | "duration" | "volume" | "muted"
>;
type PlayerActions = Pick<
  PlayerValue,
  | "play"
  | "playShuffled"
  | "enqueue"
  | "playAt"
  | "removeAt"
  | "moveQueueItem"
  | "clearQueue"
  | "toggleShuffle"
  | "cycleRepeat"
  | "retry"
  | "reloadStreamQuality"
  | "toggle"
  | "next"
  | "previous"
  | "stop"
  | "seek"
  | "changeVolume"
  | "toggleMute"
>;

const PlayerContext = createContext<PlayerValue | null>(null);
const QueueContext = createContext<MusicQueueValue | null>(null);

export function useMusicPlayer() {
  const value = useContext(PlayerContext);
  if (!value)
    throw new Error("useMusicPlayer must be used inside MusicPlayerProvider");
  return value;
}

/** Queue state and stable actions without high-frequency media progress. */
export function useMusicQueue() {
  const value = useContext(QueueContext);
  if (!value)
    throw new Error("useMusicQueue must be used inside MusicPlayerProvider");
  return value;
}

export function musicStreamURL(fileID: number) {
  return `/api/v1/library/music/files/${fileID}/stream`;
}

/**
 * Capability negotiation for browsers that cannot decode the server's default
 * streaming container (Ogg Opus). canPlayType("") means the browser reports no
 * support, so the player requests an MP3 stream the server converts on the
 * fly at the selected quality's bitrate target; other browsers use Opus.
 */
export function browserStreamSuffix(audio: HTMLAudioElement | null) {
  if (!audio || typeof audio.canPlayType !== "function") return "";
  if (audio.canPlayType('audio/ogg; codecs="opus"') === "")
    return "?compat=mp3";
  return "";
}

function newStreamID() {
  return Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
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

function validTrack(track: PlayerTrack) {
  return Number.isInteger(track.fileID) && track.fileID > 0;
}

function uniqueTracks(items: PlayerTrack[]) {
  const seen = new Set<number>();
  const result: PlayerTrack[] = [];
  for (const item of items) {
    if (!validTrack(item) || seen.has(item.fileID)) continue;
    seen.add(item.fileID);
    result.push(item);
  }
  return result;
}

function shuffledTracks(items: PlayerTrack[]) {
  const result = [...items];
  for (let index = result.length - 1; index > 0; index -= 1) {
    const other = Math.floor(Math.random() * (index + 1));
    [result[index], result[other]] = [result[other], result[index]];
  }
  return result;
}

export function MusicPlayerProvider({
  children,
  identity,
  canManageSettings = true,
}: {
  children: ReactNode;
  identity?: number;
  canManageSettings?: boolean;
}) {
  const audioRef = useRef<HTMLAudioElement>(null);
  // Monotonic counter for play() requests. A superseded load (rapid track
  // skipping) rejects its play() promise after the new track's play() has
  // already resolved; that stale rejection must not flip `playing` back to
  // false while the new track is actually playing.
  const playEpoch = useRef(0);
  // Offset responses contain only the remainder of a track. Keep their media
  // timeline separate from the full-track position shown by the player.
  const streamOffset = useRef(0);
  const sourceRequest = useRef<StreamRequest | null>(null);
  const qualityResume = useRef<QualityResume | null>(null);
  const [stream, setStream] = useState<MusicStreamStatus>({ phase: "idle" });
  const [storageKeys] = useState(() => playerStorageKeys(identity));
  const [restored] = useState(() => restorePlayer(storageKeys));
  const [current, setCurrent] = useState<PlayerTrack | null>(() =>
    restored.index >= 0 ? restored.queue[restored.index] : null,
  );
  const [queue, setQueueState] = useState<PlayerTrack[]>(restored.queue);
  const [index, setIndexState] = useState(restored.index);
  const [playing, setPlaying] = useState(false);
  const [position, setPosition] = useState(restored.position);
  const [duration, setDuration] = useState(restored.duration);
  const [volume, setVolume] = useState(restored.volume);
  const [muted, setMuted] = useState(restored.muted);
  const [shuffle, setShuffleState] = useState(restored.shuffle);
  const [repeat, setRepeatState] = useState<RepeatMode>(restored.repeat);
  const [error, setErrorState] = useState("");
  const [errorRecovery, setErrorRecovery] = useState<"" | "music-import">("");
  const currentRef = useRef(current);
  const queueRef = useRef(queue);
  const indexRef = useRef(index);
  const shuffleRef = useRef(shuffle);
  const repeatRef = useRef(repeat);
  const positionRef = useRef(position);
  const durationRef = useRef(duration);
  const volumeRef = useRef(volume);
  const mutedRef = useRef(muted);
  currentRef.current = current;
  queueRef.current = queue;
  indexRef.current = index;
  shuffleRef.current = shuffle;
  repeatRef.current = repeat;
  positionRef.current = position;
  durationRef.current = duration;
  volumeRef.current = volume;
  mutedRef.current = muted;
  const structureWriterRef = useRef<() => void>(() => {});
  const progressWriterRef = useRef<() => void>(() => {});
  const progressTimer = useRef<number | null>(null);
  const lastProgressWrite = useRef(0);

  // Keep the pagehide snapshot coherent even when selection changes and the
  // page is hidden before React commits another render.
  function setQueue(
    next: PlayerTrack[] | ((current: PlayerTrack[]) => PlayerTrack[]),
  ) {
    const resolved = typeof next === "function" ? next(queueRef.current) : next;
    queueRef.current = resolved;
    setQueueState(resolved);
  }

  function setIndex(next: number | ((current: number) => number)) {
    const resolved = typeof next === "function" ? next(indexRef.current) : next;
    indexRef.current = resolved;
    setIndexState(resolved);
  }

  function setShuffle(next: boolean) {
    shuffleRef.current = next;
    setShuffleState(next);
  }

  function setRepeat(next: RepeatMode | ((current: RepeatMode) => RepeatMode)) {
    const resolved =
      typeof next === "function" ? next(repeatRef.current) : next;
    repeatRef.current = resolved;
    setRepeatState(resolved);
  }

  function setError(message: string, recovery: "" | "music-import" = "") {
    setErrorState(message);
    setErrorRecovery(recovery);
  }

  useEffect(() => {
    document.body.classList.toggle("has-music-player", Boolean(current));
    return () => document.body.classList.remove("has-music-player");
  }, [current]);

  progressWriterRef.current = () => {
    try {
      const current = currentRef.current;
      if (!current) {
        window.localStorage.removeItem(storageKeys.progress);
        return;
      }
      window.localStorage.setItem(
        storageKeys.progress,
        JSON.stringify({
          fileID: current.fileID,
          position: positionRef.current,
          duration: durationRef.current,
          volume: volumeRef.current,
          muted: mutedRef.current,
        }),
      );
    } catch {
      // Playback remains usable when browser storage is unavailable.
    }
  };

  // Structural snapshots never serialize the live queue directly. Large
  // queues retain current + the next 499 entries and restore at index zero.
  structureWriterRef.current = () => {
    try {
      const current = currentRef.current;
      const queue = queueRef.current;
      const index = indexRef.current;
      if (!current) {
        window.localStorage.removeItem(storageKeys.queue);
        return;
      }
      const windowed =
        queue.length > PERSISTED_QUEUE_LIMIT &&
        index >= 0 &&
        index < queue.length;
      const savedQueue = windowed
        ? queue.slice(index, index + PERSISTED_QUEUE_LIMIT)
        : queue.slice(0, PERSISTED_QUEUE_LIMIT);
      window.localStorage.setItem(
        storageKeys.queue,
        JSON.stringify({
          queue: savedQueue,
          index: windowed ? 0 : index,
          // Compatibility for older builds; current values also live in the
          // independent progress/preferences record.
          position: positionRef.current,
          duration: durationRef.current,
          volume: volumeRef.current,
          muted: mutedRef.current,
          shuffle: shuffleRef.current,
          repeat: repeatRef.current,
        }),
      );
    } catch {
      // Storage may be unavailable in private/embedded browsing; playback
      // remains functional and simply does not promise restoration.
    }
  };

  useEffect(() => {
    const timer = window.setTimeout(() => structureWriterRef.current(), 250);
    return () => window.clearTimeout(timer);
  }, [current, queue, index, shuffle, repeat]);

  // Media time updates are frequent, so progress and preferences have a small
  // trailing throttle instead of rewriting the structural queue snapshot.
  useEffect(() => {
    if (progressTimer.current !== null) return;
    const elapsed = Date.now() - lastProgressWrite.current;
    progressTimer.current = window.setTimeout(
      () => {
        progressTimer.current = null;
        lastProgressWrite.current = Date.now();
        progressWriterRef.current();
      },
      Math.max(0, PROGRESS_WRITE_INTERVAL - elapsed),
    );
  }, [position, duration, volume, muted]);

  useEffect(() => {
    const flush = () => {
      if (progressTimer.current !== null) {
        window.clearTimeout(progressTimer.current);
        progressTimer.current = null;
      }
      lastProgressWrite.current = Date.now();
      structureWriterRef.current();
      progressWriterRef.current();
    };
    window.addEventListener("pagehide", flush);
    return () => {
      window.removeEventListener("pagehide", flush);
      if (progressTimer.current !== null)
        window.clearTimeout(progressTimer.current);
    };
  }, []);

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

  function loadSource(audio: HTMLAudioElement, fileID: number, offset = 0) {
    const source = new URL(
      musicStreamURL(fileID) + browserStreamSuffix(audio),
      window.location.origin,
    );
    const id = newStreamID();
    source.searchParams.set("stream_id", id);
    if (offset > 0) source.searchParams.set("timeOffset", String(offset));
    sourceRequest.current = { fileID, id, checking: false };
    streamOffset.current = offset;
    setStream({ phase: "loading" });
    audio.src = source.pathname + source.search;
    audio.load();
  }

  function clearStream() {
    sourceRequest.current = null;
    qualityResume.current = null;
    setStream({ phase: "idle" });
  }

  function start(track: PlayerTrack, offset = 0) {
    const audio = audioRef.current;
    if (!audio) return;
    const resumeAt = Math.max(0, offset);
    qualityResume.current = null;
    currentRef.current = track;
    setCurrent(track);
    positionRef.current = resumeAt;
    setPosition(resumeAt);
    const nextDuration =
      resumeAt > 0 && durationRef.current > resumeAt
        ? durationRef.current
        : track.durationMS
          ? track.durationMS / 1000
          : 0;
    durationRef.current = nextDuration;
    setDuration(nextDuration);
    setError("");
    loadSource(audio, track.fileID, Math.floor(resumeAt));
    requestPlay(audio);
  }

  function play(track: PlayerTrack, requestedQueue?: PlayerTrack[]) {
    if (!validTrack(track)) return;
    const list = uniqueTracks(requestedQueue ?? []);
    let nextQueue = list.length ? list : [track];
    let nextIndex = nextQueue.findIndex((item) => item.fileID === track.fileID);
    if (nextIndex < 0) {
      nextQueue = [track, ...nextQueue];
      nextIndex = 0;
    }
    const selected = nextQueue[nextIndex];
    if (shuffle) {
      // Ordinary play remains coherent in shuffle mode: the chosen track is
      // first and only the unplayed remainder is shuffled.
      nextQueue = [
        selected,
        ...shuffledTracks(
          nextQueue.filter((_, itemIndex) => itemIndex !== nextIndex),
        ),
      ];
      nextIndex = 0;
    }
    setQueue(nextQueue);
    setIndex(nextIndex);
    if (current?.fileID === selected.fileID && audioRef.current) {
      if (!audioRef.current.getAttribute("src")) {
        start(selected);
        return;
      }
      if (streamOffset.current > 0) {
        start(selected);
        return;
      }
      positionRef.current = 0;
      setPosition(0);
      audioRef.current.currentTime = 0;
      setError("");
      requestPlay(audioRef.current);
      return;
    }
    start(selected);
  }

  function playShuffled(items: PlayerTrack[]) {
    const nextQueue = shuffledTracks(uniqueTracks(items));
    if (!nextQueue.length) return;
    setQueue(nextQueue);
    setIndex(0);
    setShuffle(true);
    start(nextQueue[0]);
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
    clearStream();
    audio?.pause();
    if (audio?.getAttribute("src")) {
      audio.removeAttribute("src");
      audio.load();
    }
    streamOffset.current = 0;
    currentRef.current = track;
    setCurrent(track);
    setPlaying(false);
    positionRef.current = 0;
    setPosition(0);
    const nextDuration = track.durationMS ? track.durationMS / 1000 : 0;
    durationRef.current = nextDuration;
    setDuration(nextDuration);
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
    if (shuffle) {
      // Shuffle installs a visible order; disabling it never reorders tracks.
      setShuffle(false);
      return;
    }
    setShuffle(true);
    if (!queue.length) return;
    if (!current || index < 0) {
      setQueue(shuffledTracks(queue));
      return;
    }
    // Played history, the current selection and its index stay in place.
    setQueue([
      ...queue.slice(0, index + 1),
      ...shuffledTracks(queue.slice(index + 1)),
    ]);
  }

  function cycleRepeat() {
    setRepeat((value) =>
      value === "off" ? "all" : value === "all" ? "one" : "off",
    );
  }

  function retry() {
    if (current) start(current, positionRef.current);
  }

  function reloadStreamQuality() {
    const audio = audioRef.current;
    const track = currentRef.current;
    if (!track || !audio?.getAttribute("src")) return;
    const resume = qualityResume.current ?? {
      position:
        audio.readyState >= HTMLMediaElement.HAVE_METADATA
          ? streamOffset.current + audio.currentTime
          : positionRef.current,
      play: !audio.paused,
    };
    playEpoch.current += 1;
    setError("");
    positionRef.current = resume.position;
    setPosition(resume.position);
    qualityResume.current = resume.position > 0 ? resume : null;
    loadSource(audio, track.fileID);
    if (resume.play && !qualityResume.current) requestPlay(audio);
  }

  function confirmStream(audio: HTMLAudioElement) {
    const request = sourceRequest.current;
    if (!request || request.checking) return;
    request.checking = true;

    const apply = (info: MusicStreamInfo | null) => {
      if (sourceRequest.current !== request) return;
      const resume = qualityResume.current;
      qualityResume.current = null;
      if (resume) {
        if (
          resume.position > 0 &&
          (info?.range_seekable || seekableContains(audio, resume.position))
        ) {
          audio.currentTime = resume.position;
        } else if (resume.position >= 1) {
          // A live response has no byte ranges. Reopen at the media offset
          // and confirm the replacement stream once its metadata arrives.
          loadSource(audio, request.fileID, Math.floor(resume.position));
          if (resume.play) requestPlay(audio);
          return;
        }
        if (resume.play) requestPlay(audio);
      }
      setStream(info ? { phase: "ready", info } : { phase: "unavailable" });
    };

    void api<MusicStreamInfo>(
      `/library/music/files/${request.fileID}/stream-info?stream_id=${request.id}`,
      { signal: AbortSignal.timeout(5000) },
    ).then(
      (info) => apply(typeof info?.codec === "string" ? info : null),
      () => apply(null),
    );
  }

  function toggle() {
    const audio = audioRef.current;
    if (!audio || !current) return;
    if (qualityResume.current) {
      qualityResume.current.play = !qualityResume.current.play;
      setPlaying(qualityResume.current.play);
      return;
    }
    if (audio.paused) {
      setError("");
      if (!audio.getAttribute("src")) {
        start(current, positionRef.current);
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
    positionRef.current = 0;
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
        positionRef.current = 0;
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
      positionRef.current = 0;
      setPosition(0);
    }
  }

  function clearQueue() {
    const audio = audioRef.current;
    // A late play() resolution from the cleared selection must not resurrect
    // the playing state after the audio element has been unloaded.
    playEpoch.current += 1;
    clearStream();
    if (audio?.getAttribute("src")) {
      audio.pause();
      audio.removeAttribute("src");
      audio.load();
    }
    try {
      window.localStorage.removeItem(storageKeys.queue);
      window.localStorage.removeItem(storageKeys.progress);
    } catch {
      // Playback teardown must not fail when browser storage is unavailable.
    }
    streamOffset.current = 0;
    currentRef.current = null;
    positionRef.current = 0;
    durationRef.current = 0;
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
      durationRef.current ||
      (current.durationMS ? current.durationMS / 1000 : 0);
    const value = Math.max(0, Math.min(seconds, total || seconds));
    if (qualityResume.current) {
      qualityResume.current.position = value;
      positionRef.current = value;
      setPosition(value);
      return;
    }
    const relative = value - streamOffset.current;
    if (relative >= 0 && seekableContains(audio, relative)) {
      // File-backed direct/cache responses expose normal browser ranges. The
      // element time is relative to any offset response already loaded.
      audio.currentTime = relative;
      positionRef.current = value;
      setPosition(value);
      return;
    }

    // A live FFmpeg pipe cannot satisfy an unbuffered byte-range seek. Reload
    // from a media-time offset instead of pretending currentTime succeeded.
    const resume = !audio.paused;
    loadSource(audio, current.fileID, Math.floor(value));
    positionRef.current = value;
    setPosition(value);
    setError("");
    if (resume) requestPlay(audio);
  }

  function changeVolume(value: number) {
    const nextVolume = Math.max(0, Math.min(1, value));
    volumeRef.current = nextVolume;
    setVolume(nextVolume);
    if (value > 0) {
      mutedRef.current = false;
      setMuted(false);
    }
  }

  function toggleMute() {
    const nextMuted = !mutedRef.current;
    mutedRef.current = nextMuted;
    setMuted(nextMuted);
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
    const updatePosition = () => {
      if (qualityResume.current) return;
      const nextPosition = streamOffset.current + (audio.currentTime || 0);
      positionRef.current = nextPosition;
      setPosition(nextPosition);
    };
    const updateDuration = () => {
      if (Number.isFinite(audio.duration) && audio.duration > 0) {
        const nextDuration = streamOffset.current + audio.duration;
        durationRef.current = nextDuration;
        setDuration(nextDuration);
      }
    };
    const onMetadata = () => {
      if (audio.currentSrc && audio.currentSrc !== audio.src) return;
      updateDuration();
      confirmStream(audio);
    };
    const onPlay = () => setPlaying(true);
    const onPause = () => {
      if (!qualityResume.current) setPlaying(false);
    };
    const onEnded = () => advance(true);
    const onError = () => {
      if (audio.currentSrc && audio.currentSrc !== audio.src) return;
      sourceRequest.current = null;
      qualityResume.current = null;
      setStream({ phase: "unavailable" });
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
          if (audio.src !== source) return;
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
          if (audio.src !== source) return;
          setError("Playback stopped because ReelScarey could not be reached.");
        },
      );
    };
    audio.addEventListener("timeupdate", updatePosition);
    audio.addEventListener("durationchange", updateDuration);
    audio.addEventListener("loadedmetadata", onMetadata);
    audio.addEventListener("play", onPlay);
    audio.addEventListener("pause", onPause);
    audio.addEventListener("ended", onEnded);
    audio.addEventListener("error", onError);
    return () => {
      audio.removeEventListener("timeupdate", updatePosition);
      audio.removeEventListener("durationchange", updateDuration);
      audio.removeEventListener("loadedmetadata", onMetadata);
      audio.removeEventListener("play", onPlay);
      audio.removeEventListener("pause", onPause);
      audio.removeEventListener("ended", onEnded);
      audio.removeEventListener("error", onError);
    };
  }, [current, queue, index, shuffle, repeat]);

  // Expose stable action identities to queue-only consumers while routing each
  // call through the latest render's closure.
  const actionsRef = useRef<PlayerActions | null>(null);
  actionsRef.current = {
    play,
    playShuffled,
    enqueue,
    playAt,
    removeAt,
    moveQueueItem,
    clearQueue,
    toggleShuffle,
    cycleRepeat,
    retry,
    reloadStreamQuality,
    toggle,
    next,
    previous,
    stop,
    seek,
    changeVolume,
    toggleMute,
  };
  const actions = useMemo<PlayerActions>(
    () => ({
      play: (track, requestedQueue) =>
        actionsRef.current!.play(track, requestedQueue),
      playShuffled: (items) => actionsRef.current!.playShuffled(items),
      enqueue: (track, playNext) =>
        actionsRef.current!.enqueue(track, playNext),
      playAt: (requestedIndex) => actionsRef.current!.playAt(requestedIndex),
      removeAt: (requestedIndex) =>
        actionsRef.current!.removeAt(requestedIndex),
      moveQueueItem: (from, to) => actionsRef.current!.moveQueueItem(from, to),
      clearQueue: () => actionsRef.current!.clearQueue(),
      toggleShuffle: () => actionsRef.current!.toggleShuffle(),
      cycleRepeat: () => actionsRef.current!.cycleRepeat(),
      retry: () => actionsRef.current!.retry(),
      reloadStreamQuality: () => actionsRef.current!.reloadStreamQuality(),
      toggle: () => actionsRef.current!.toggle(),
      next: () => actionsRef.current!.next(),
      previous: () => actionsRef.current!.previous(),
      stop: () => actionsRef.current!.stop(),
      seek: (seconds) => actionsRef.current!.seek(seconds),
      changeVolume: (nextVolume) =>
        actionsRef.current!.changeVolume(nextVolume),
      toggleMute: () => actionsRef.current!.toggleMute(),
    }),
    [],
  );
  const queueValue = useMemo<MusicQueueValue>(
    () => ({
      current,
      queue,
      index,
      playing,
      shuffle,
      repeat,
      error,
      errorRecovery,
      canManageSettings,
      stream,
      ...actions,
    }),
    [
      current,
      queue,
      index,
      playing,
      shuffle,
      repeat,
      error,
      errorRecovery,
      canManageSettings,
      stream,
      actions,
    ],
  );
  const effectiveDuration =
    duration || (current?.durationMS ? current.durationMS / 1000 : 0);
  const value = useMemo<PlayerValue>(
    () => ({
      ...queueValue,
      position,
      duration: effectiveDuration,
      volume,
      muted,
    }),
    [queueValue, position, effectiveDuration, volume, muted],
  );

  return (
    <PlayerContext.Provider value={value}>
      <QueueContext.Provider value={queueValue}>
        {children}
        <audio
          ref={audioRef}
          preload="metadata"
          className="music-player-audio"
        />
      </QueueContext.Provider>
    </PlayerContext.Provider>
  );
}

function streamQualityStatus(stream: MusicStreamStatus, playing: boolean) {
  if (stream.phase === "loading") return "Opening stream…";
  if (stream.phase === "unavailable") return "Stream details unavailable";
  if (stream.phase !== "ready") return "";
  const { codec, bitrate_kbps: bitrate, mode } = stream.info;
  const format = codec === "opus" ? "Opus" : codec.toUpperCase() || "audio";
  const quality =
    mode === "direct"
      ? `Original ${format}`
      : format + (bitrate ? ` ${bitrate} kbps` : "");
  return `${playing ? "Playing" : "Stream"} · ${quality}`;
}

function PlayerQualityControl() {
  const {
    playing,
    stream,
    reloadStreamQuality,
    canManageSettings = true,
  } = useMusicQueue();
  const prefs = useResource<MusicPreferences>("/library/music/preferences", {
    retain: true,
  });
  const [value, setValue] = useState<MusicStreamingQuality>("balanced");
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");

  useEffect(() => {
    if (prefs.data) setValue(prefs.data.streaming_quality);
  }, [prefs.data]);

  async function change(next: MusicStreamingQuality) {
    if (!canManageSettings) return;
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
      const saved = await api<MusicPreferences>("/library/music/preferences", {
        method: "PUT",
        body: JSON.stringify({ ...prefs.data, streaming_quality: next }),
      });
      prefs.setData(saved);
      if (saved.streaming_quality !== prefs.data.streaming_quality)
        reloadStreamQuality();
      prefs.reload();
    } catch (e) {
      setValue(prefs.data.streaming_quality);
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
        disabled={!canManageSettings || saving || prefs.loading || !prefs.data}
        title={
          canManageSettings
            ? undefined
            : "Streaming quality is managed by an administrator"
        }
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
      <span className="music-player-meta music-player-stream" role="status">
        {streamQualityStatus(stream, playing)}
      </span>
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
          {track.albumID && player.canManageSettings !== false ? (
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
              {player.errorRecovery === "music-import" &&
                player.canManageSettings !== false && (
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
      <div className="music-player-end">
        <PlayerQualityControl />
        <div className="music-player-volume">
          <Link className="music-player-open-full" to="/music/player">
            Open full player
          </Link>
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
            onChange={(event) =>
              player.changeVolume(Number(event.target.value))
            }
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
      </div>
    </section>
  );
}
