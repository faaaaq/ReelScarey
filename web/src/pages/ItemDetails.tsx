import {
  Bookmark,
  FolderTree,
  RefreshCw,
  Search,
  Settings2,
} from "lucide-react";
import { useParams } from "react-router-dom";
import { RequestState } from "../components";
import { Pagination } from "../movieComponents";
import {
  Alert,
  Label,
  PageBody,
  PageToolbar,
  ToolbarButton,
  ToolbarSection,
  ToolbarSeparator,
  formatBytes,
} from "../ui";
import { AnimeSettings } from "./AnimeSettings";
import { AnimeOrder } from "./AnimeOrder";
import { ItemOrganization } from "./Organization";
import { MovieCollection } from "./MovieCollection";
import { RelatedTitles } from "./RelatedTitles";
import {
  ItemSearchResultAlert,
  TitleSearchButton,
  useItemSearch,
} from "./details/ItemSearch";
import { AutomaticFranchise } from "./AutomaticFranchise";
import { MovieVersions } from "./MovieVersions";
import { WatchProviders } from "./WatchProviders";
import {
  CreditsSection,
  DetailFact,
  DetailHeader,
  DetailTabs,
  FilesSection,
  HistorySection,
  MetadataError,
  ProviderLinks,
  profileLabel,
} from "./details/DetailParts";
import { useItemDetails } from "./details/useItemDetails";

export type { Information, LocalDetails } from "./details/useItemDetails";

export function ItemDetails() {
  const { id } = useParams();
  return id && /^[1-9][0-9]*$/.test(id) && Number.isSafeInteger(Number(id)) ? (
    <MovieView key={id} id={Number(id)} />
  ) : (
    <PageBody>
      <Alert kind="danger">Choose an item from your library.</Alert>
    </PageBody>
  );
}

function MovieView({ id }: { id: number }) {
  const details = useItemDetails(id);
  const data = details.data;
  const item = data?.item;
  const search = useItemSearch();
  return (
    <>
      <PageToolbar>
        <ToolbarSection>
          <ToolbarButton
            icon={RefreshCw}
            label="Refresh"
            title="Refresh metadata from TMDb"
            spinning={details.busy}
            disabled={details.busy || !data}
            onClick={() => void details.refresh()}
          />
          {item?.kind === "movie" && (
            <ToolbarButton
              icon={Search}
              label="Manual Search"
              to={`/releases?movie=${id}`}
            />
          )}
          <TitleSearchButton
            mediaID={Number(id)}
            pending={!!search.pending}
            onTrigger={(mediaID) => search.trigger(mediaID)}
          />
          <ToolbarSeparator />
          <ToolbarButton
            icon={FolderTree}
            label="Organize"
            to={`/organization?media_id=${id}`}
          />
          <ToolbarButton icon={Settings2} label="Automation" to="/automation" />
        </ToolbarSection>
      </PageToolbar>
      {search.result && (
        <PageBody>
          <ItemSearchResultAlert
            result={search.result}
            onDismiss={search.reset}
          />
        </PageBody>
      )}
      {!data ? (
        <PageBody>
          <RequestState
            loading={details.loading}
            error={details.loadError}
            retry={details.reload}
          />
        </PageBody>
      ) : (
        <>
          <DetailHeader
            item={data.item}
            title={data.item.title}
            monitor={
              <span
                className={`det-monitor-static${data.item.monitored ? " is-on" : ""}`}
                title={data.item.monitored ? "Monitored" : "Unmonitored"}
              >
                <Bookmark
                  size={24}
                  fill={data.item.monitored ? "currentColor" : "none"}
                  aria-hidden="true"
                />
              </span>
            }
            facts={[
              data.item.year,
              data.item.runtime_minutes
                ? `${data.item.runtime_minutes} min`
                : null,
              details.info?.genres.length
                ? details.info.genres.join(", ")
                : null,
            ]}
            labels={
              <>
                {data.file_count > 0 ? (
                  <Label kind="success" large>
                    On disk
                  </Label>
                ) : data.item.status === "downloading" ? (
                  <Label kind="info" large>
                    Downloading
                  </Label>
                ) : (
                  <Label
                    kind={data.item.monitored ? "danger" : "disabled"}
                    large
                  >
                    Missing
                  </Label>
                )}
                <Label kind={data.item.monitored ? "primary" : "disabled"}>
                  {data.item.monitored ? "Monitored" : "Unmonitored"}
                </Label>
                {data.item.category === "anime" && (
                  <Label kind="purple">Anime</Label>
                )}
                {profileLabel(data.item)}
                {details.info?.rating ? (
                  <Label outline>TMDb {details.info.rating.toFixed(1)}</Label>
                ) : null}
              </>
            }
            details={
              <>
                <DetailFact title="Path">
                  <code>{data.library_root}</code>
                </DetailFact>
                <DetailFact title="Size">
                  {data.total_size_bytes
                    ? formatBytes(data.total_size_bytes)
                    : "No files"}
                </DetailFact>
                <DetailFact title="Files">{data.file_count}</DetailFact>
                <DetailFact title="Downloads">{data.history_count}</DetailFact>
              </>
            }
            links={
              <ProviderLinks
                details={details}
                kind={data.item.kind}
                tmdbID={data.item.tmdb_id}
              />
            }
          />
          <PageBody>
            {details.info?.tagline && (
              <p className="rs-muted">{details.info.tagline}</p>
            )}
            <MetadataError details={details} />
            {details.loadError && (
              <RequestState
                loading={false}
                error={details.loadError}
                retry={details.reload}
              />
            )}
            <DetailTabs
              label="Movie sections"
              tabs={[
                {
                  value: "details",
                  label: "Details",
                  content: (
                    <>
                      <FilesSection
                        details={details}
                        savedTitle={data.item.title}
                      />
                      {data.item.kind === "movie" && <MovieVersions id={id} />}
                      {data.item.kind === "movie" && (
                        <WatchProviders id={data.item.tmdb_id} />
                      )}
                      <HistorySection details={details} />
                      <Pagination
                        page={details.page}
                        pages={Math.ceil(
                          Math.max(data.file_count, data.history_count) / 50,
                        )}
                        onPage={details.setPage}
                      />
                      <CreditsSection details={details} />
                    </>
                  ),
                },
                {
                  value: "related",
                  label: "Related",
                  content: (
                    <>
                      {data.item.kind === "movie" && (
                        <MovieCollection id={id} />
                      )}
                      <AutomaticFranchise id={id} />
                      <RelatedTitles id={id} />
                    </>
                  ),
                },
                {
                  value: "settings",
                  label: "Settings",
                  content: (
                    <>
                      <AnimeSettings id={id} onSaved={details.reload} />
                      {data.item.category === "anime" && <AnimeOrder id={id} />}
                      <ItemOrganization id={id} />
                    </>
                  ),
                },
              ]}
            />
          </PageBody>
        </>
      )}
    </>
  );
}
