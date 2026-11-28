export function artistImageURL(artistID?: number | null, saved?: string) {
  const local = artistID
    ? `/api/v1/library/music/artists/${artistID}/image`
    : "";
  return saved?.startsWith(`${local}?v=`) && local ? saved : local;
}

export function albumCoverURL(albumID?: number | null, saved?: string) {
  const local = albumID ? `/api/v1/library/music/albums/${albumID}/cover` : "";
  return saved?.startsWith(`${local}?v=`) && local ? saved : local;
}

export function withArtworkVersion(url: string, version: number) {
  return url && version && !url.includes("?") ? `${url}?v=${version}` : url;
}
