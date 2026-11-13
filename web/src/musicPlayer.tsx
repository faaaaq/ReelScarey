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
  SkipBack,
  SkipForward,
  Volume2,
  VolumeX,
  X,
} from "lucide-react";
import { Link } from "react-router-dom";
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
};

type PlayerValue = {
  current: PlayerTrack | null;
  queue: PlayerTrack[];
  playing: boolean;
  position: number;
  duration: number;
  volume: number;
  muted: boolean;
  error: string;
  play: (track: PlayerTrack, queue?: PlayerTrack[]) => void;
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

function clock(seconds: number) {
  if (!Number.isFinite(seconds) || seconds <= 0) return "0:00";
  const whole = Math.floor(seconds);
  return `${Math.floor(whole / 60)}:${String(whole % 60).padStart(2, "0")}`;
}

export function MusicPlayerProvider({ children }: { children: ReactNode }) {
  const audioRef = useRef<HTMLAudioElement>(null);
  const [current, setCurrent] = useState<PlayerTrack | null>(null);
  const [queue, setQueue] = useState<PlayerTrack[]>([]);
  const [index, setIndex] = useState(-1);
  const [playing, setPlaying] = useState(false);
  const [position, setPosition] = useState(0);
  const [duration, setDuration] = useState(0);
  const [volume, setVolume] = useState(0.8);
  const [muted, setMuted] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    document.body.classList.toggle("has-music-player", Boolean(current));
    return () => document.body.classList.remove("has-music-player");
  }, [current]);

  function start(track: PlayerTrack) {
    const audio = audioRef.current;
    if (!audio) return;
    setCurrent(track);
    setPosition(0);
    setDuration(track.durationMS ? track.durationMS / 1000 : 0);
    setError("");
    audio.src = musicStreamURL(track.fileID);
    audio.load();
    void audio
      .play()
      .then(() => setPlaying(true))
      .catch((e: unknown) => {
        setPlaying(false);
        // Autoplay policies reject programmatic play before any request is
        // made; that is not a server failure and must be phrased as one the
        // user can act on (RS-0008).
        if (e instanceof DOMException && e.name === "NotAllowedError")
          setError("The browser blocked autoplay. Press play to start.");
      });
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
      setPosition(0);
      audioRef.current.currentTime = 0;
      setError("");
      void audioRef.current
        .play()
        .then(() => setPlaying(true))
        .catch((e: unknown) => {
          setPlaying(false);
          if (e instanceof DOMException && e.name === "NotAllowedError")
            setError("The browser blocked autoplay. Press play to start.");
        });
      return;
    }
    start(track);
  }

  function toggle() {
    const audio = audioRef.current;
    if (!audio || !current) return;
    if (audio.paused) {
      setError("");
      void audio
        .play()
        .then(() => setPlaying(true))
        .catch((e: unknown) => {
          setPlaying(false);
          if (e instanceof DOMException && e.name === "NotAllowedError")
            setError("The browser blocked autoplay. Press play to start.");
        });
    } else {
      audio.pause();
    }
  }

  function next() {
    if (index < 0 || index + 1 >= queue.length) {
      audioRef.current?.pause();
      setPlaying(false);
      setPosition(0);
      return;
    }
    const nextIndex = index + 1;
    setIndex(nextIndex);
    start(queue[nextIndex]);
  }

  function previous() {
    const audio = audioRef.current;
    if (!audio) return;
    if (audio.currentTime > 3) {
      audio.currentTime = 0;
      setPosition(0);
      return;
    }
    if (index > 0) {
      const previousIndex = index - 1;
      setIndex(previousIndex);
      start(queue[previousIndex]);
      return;
    }
    audio.currentTime = 0;
    setPosition(0);
  }

  function stop() {
    const audio = audioRef.current;
    if (audio) {
      audio.pause();
      audio.removeAttribute("src");
      audio.load();
    }
    setCurrent(null);
    setQueue([]);
    setIndex(-1);
    setPlaying(false);
    setPosition(0);
    setDuration(0);
    setError("");
  }

  function seek(seconds: number) {
    const audio = audioRef.current;
    if (!audio) return;
    const value = Math.max(0, Math.min(seconds, audio.duration || seconds));
    audio.currentTime = value;
    setPosition(value);
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
    const updatePosition = () => setPosition(audio.currentTime || 0);
    const updateDuration = () => {
      if (Number.isFinite(audio.duration) && audio.duration > 0) {
        setDuration(audio.duration);
      }
    };
    const onPlay = () => setPlaying(true);
    const onPause = () => setPlaying(false);
    const onEnded = () => next();
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
        (response) => {
          if (response.status === 401) {
            window.dispatchEvent(new Event("reelscarey:unauthorized"));
            setError("Sign in again to keep listening.");
          } else if (response.status === 403) {
            setError("This account cannot play music files.");
          } else if (response.status === 404) {
            setError("This file is no longer registered in the music library.");
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
  }, [queue, index]);

  const value: PlayerValue = {
    current,
    queue,
    playing,
    position,
    duration: duration || (current?.durationMS ? current.durationMS / 1000 : 0),
    volume,
    muted,
    error,
    play,
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

export function MusicPlayerBar() {
  const player = useMusicPlayer();
  const track = player.current;
  if (!track) return null;
  const total = Math.max(0, player.duration);
  const position = Math.min(Math.max(0, player.position), total || 0);
  return (
    <section className="music-player" aria-label="Music player">
      <div className="music-player-track">
        {track.coverURL ? (
          <img
            className="music-player-cover"
            src={track.coverURL}
            alt=""
            referrerPolicy="no-referrer"
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
        </div>
      </div>
      <div className="music-player-center">
        <div className="music-player-buttons">
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
        </div>
        <div className="music-player-seek">
          <span>{clock(position)}</span>
          <input
            type="range"
            aria-label="Seek"
            aria-disabled={!total}
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
