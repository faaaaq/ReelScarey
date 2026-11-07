import { api } from "../../api";

/** POSTs to a Torrents endpoint that answers 204 No Content. */
export async function postNoContent(path: string, body: unknown) {
  await api<unknown>(path, { method: "POST", body: JSON.stringify(body) });
}
