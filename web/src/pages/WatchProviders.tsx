import { ExternalLink } from "lucide-react";
import { Badge, Link as ThemeLink } from "@radix-ui/themes";
import { useResource } from "../useResource";
import type { WatchOffer, WatchProviders as WatchProvidersData } from "../api";
import { Panel } from "../ui";

function Offer({ offer }: { offer: WatchOffer }) {
  return (
    <Badge variant="soft" size="2">
      {offer.logo_url ? (
        <img
          src={offer.logo_url}
          alt=""
          width={18}
          height={18}
          loading="lazy"
          style={{ borderRadius: 4, marginRight: 5 }}
        />
      ) : null}
      {offer.name}
    </Badge>
  );
}

function Group({ title, offers }: { title: string; offers: WatchOffer[] }) {
  if (!offers.length) return null;
  return (
    <div style={{ marginBottom: 10 }}>
      <div className="rs-muted rs-small" style={{ marginBottom: 5 }}>
        {title}
      </div>
      <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
        {offers.map((offer) => (
          <Offer key={`${title}-${offer.name}`} offer={offer} />
        ))}
      </div>
    </div>
  );
}

/**
 * Region-scoped TMDb availability. It is a suggestion, not a guarantee that a
 * provider still carries the title or that playback is licensed in the region.
 */
export function WatchProviders({ id }: { id: number }) {
  const resource = useResource<WatchProvidersData>(
    `/metadata/movies/${id}/watch?region=US`,
  );
  const data = resource.data;
  if (!data) return null;
  const { flatrate, rent, buy, free, link } = data.providers;
  if (!flatrate.length && !rent.length && !buy.length && !free.length) {
    return null;
  }
  return (
    <Panel title="Where to watch (US)">
      <p className="rs-muted rs-small">
        Availability from TMDb for the United States. Provider catalogs and
        licensing can change; ReelScarey does not stream or verify playback.
      </p>
      <Group title="Included with a subscription" offers={flatrate} />
      <Group title="Free" offers={free} />
      <Group title="Rent" offers={rent} />
      <Group title="Buy" offers={buy} />
      {link ? (
        <ThemeLink href={link} target="_blank" rel="noreferrer">
          <ExternalLink size={14} aria-hidden="true" /> Open on TMDb
        </ThemeLink>
      ) : null}
    </Panel>
  );
}
