import { useEffect, useRef, useState } from "react";
import { api, messageFor, type MediaItem } from "../../api";
import { useResource } from "../../useResource";

export type Information = {
  tagline: string;
  genres: string[];
  rating: number;
  votes: number;
  imdb_url: string;
  tmdb_url: string;
  trailer_url: string;
  cast: { id: number; name: string; role: string; image_url: string }[];
  crew: { id: number; name: string; role: string; image_url: string }[];
};
export type DetailFile = {
  version_label?: string;
  origin?: string;
  id: number;
  path: string;
  directory: string;
  size_bytes: number;
  imported_at: number;
  download_id: string;
  release: string;
  episodes: string;
  probe: {
    codec: string;
    width: number;
    height: number;
    duration_seconds: number;
    audio_streams?: { codec: string; language: string; title: string }[] | null;
    embedded_title?: string | null;
  };
};
export type DetailJob = {
  protocol?: "torrent" | "usenet";
  id: string;
  title: string;
  indexer: string;
  state: string;
  progress: number;
  size_bytes: number;
  created_at: number;
  updated_at: number;
  error: string;
};
export type LocalDetails = {
  item: MediaItem;
  information: Information | null;
  metadata_updated_at: number;
  media_root: string;
  library_root: string;
  total_size_bytes: number;
  file_count: number;
  history_count: number;
  page: number;
  files: DetailFile[];
  history: DetailJob[];
};

/**
 * Saved local details for one catalog item: files, history and cached TMDb
 * information. Stale or missing information is refreshed once on load; the
 * previous page stays visible while a reload is in flight.
 */
export function useItemDetails(id: number) {
  const [page, setPage] = useState(1);
  const resource = useResource<LocalDetails>(
    `/library/items/${id}?page=${page}`,
  );
  const [last, setLast] = useState<LocalDetails>();
  const [fresh, setFresh] = useState<{
    information: Information;
    metadata_updated_at: number;
  }>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [inspecting, setInspecting] = useState<number>();
  const [inspectionError, setInspectionError] = useState<{
    id: number;
    message: string;
  }>();
  const attempted = useRef(false);

  useEffect(() => {
    if (resource.data) setLast(resource.data);
  }, [resource.data]);

  async function refresh() {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      setFresh(
        await api(`/library/items/${id}/metadata`, {
          method: "POST",
          body: "{}",
        }),
      );
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  async function inspect(fileID: number) {
    if (inspecting) return;
    setInspecting(fileID);
    setInspectionError(undefined);
    try {
      await api(`/library/files/${fileID}/inspect`, {
        method: "POST",
        body: "{}",
      });
      resource.reload();
    } catch (e) {
      setInspectionError({ id: fileID, message: messageFor(e) });
    } finally {
      setInspecting(undefined);
    }
  }
  useEffect(() => {
    if (!resource.data || attempted.current) return;
    attempted.current = true;
    if (
      !resource.data.information ||
      Date.now() / 1000 - resource.data.metadata_updated_at > 86400
    )
      void refresh();
  }, [resource.data]);

  const data = resource.data ?? last;
  return {
    data,
    loading: resource.loading && !data,
    loadError: resource.error,
    reload: resource.reload,
    page,
    setPage,
    info: fresh?.information ?? data?.information ?? undefined,
    updated: fresh?.metadata_updated_at ?? data?.metadata_updated_at,
    busy,
    error,
    refresh,
    inspect,
    inspecting,
    inspectionError,
  };
}

export type ItemDetailsState = ReturnType<typeof useItemDetails>;
