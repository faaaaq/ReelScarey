import { Discover } from "./youtube/Discover";

/** `/youtube` route component. The YouTube surface moved to
 * `pages/youtube/`; this wrapper keeps the route and existing imports stable. */
export function YouTube() {
  return <Discover />;
}

export * from "./youtube/types";
export * from "./youtube/status";
