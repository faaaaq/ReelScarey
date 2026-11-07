import { Button } from "@radix-ui/themes";
import { ChevronLeft, ChevronRight } from "lucide-react";

/** Previous/next page control shared by the Activity, Wanted and Calendar tables. */
export function Pager({
  page,
  pages,
  onPage,
}: {
  page: number;
  pages: number;
  onPage: (page: number) => void;
}) {
  if (pages <= 1) return null;
  return (
    <nav className="rs-pagination" aria-label="Pagination">
      <Button
        size="1"
        variant="soft"
        color="gray"
        disabled={page <= 1}
        onClick={() => onPage(page - 1)}
      >
        <ChevronLeft size={14} aria-hidden="true" /> Previous
      </Button>
      <span>
        Page {page} of {pages}
      </span>
      <Button
        size="1"
        variant="soft"
        color="gray"
        disabled={page >= pages}
        onClick={() => onPage(page + 1)}
      >
        Next <ChevronRight size={14} aria-hidden="true" />
      </Button>
    </nav>
  );
}
