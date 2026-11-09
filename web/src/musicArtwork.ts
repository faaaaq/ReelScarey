export function artistImageURL(artistID?: number | null) {
  return artistID ? `/api/v1/library/music/artists/${artistID}/image` : "";
}

export function albumCoverURL(albumID?: number | null) {
  return albumID ? `/api/v1/library/music/albums/${albumID}/cover` : "";
}
