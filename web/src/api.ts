export type Settings = {
  library_name: string;
  movie_folder: string;
  series_folder: string;
  revision: number;
  updated_at: string;
};

export type SettingsUpdate = Omit<Settings, "updated_at">;
export type System = {
  name: string;
  version: string;
  milestone: string;
  database: string;
  schema_version: number;
  uptime_seconds: number;
  paths: { config: string; media: string; downloads: string };
  capabilities: Record<string, boolean>;
};
export type Dashboard = {
  library_name: string;
  counts: { movies: number; series: number };
  settings_revision: number;
  settings_updated_at: string;
};
export type MediaItem = {
  id: number;
  kind: "movie" | "series";
  tmdb_id: number;
  title: string;
  year: number | null;
  monitored: boolean;
  status: string;
};

export class APIError extends Error {
  constructor(
    message: string,
    public status: number,
  ) {
    super(message);
  }
}

export async function api<T>(
  path: string,
  options: RequestInit = {},
): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
    ...options,
    headers: {
      ...(options.body ? { "Content-Type": "application/json" } : {}),
      ...options.headers,
    },
    signal: options.signal ?? AbortSignal.timeout(15000),
  });
  const body = await response.json();
  if (!response.ok)
    throw new APIError(
      body.error?.message ?? "The request could not be completed.",
      response.status,
    );
  return body as T;
}

export function messageFor(error: unknown): string {
  if (error instanceof APIError) return error.message;
  return "ReelScarey could not be reached. Check that the server is running, then try again.";
}
