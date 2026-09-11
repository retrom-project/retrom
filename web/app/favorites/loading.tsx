import { PageHeader } from "@/components/ui";

export default function FavoritesLoading() {
  return (
    <div className="page-layout favorite-page favorite-loading-shell" role="status" aria-label="正在加载收藏" aria-live="polite">
      <span className="sr-only">正在加载收藏</span>
      <PageHeader eyebrow="你的游戏" title="我的收藏" description="" />
      <div className="favorite-head-summary favorite-skeleton-line" aria-hidden="true" />
      <div className="favorite-layout" aria-hidden="true">
        <section className="favorite-content">
          <div className="favorite-toolbar"><div className="favorite-skeleton-control" /><div className="favorite-skeleton-control" /><div className="favorite-skeleton-control" /></div>
          <div className="favorite-platforms"><div className="favorite-skeleton-line" /></div>
          <div className="favorite-game-grid">
            {Array.from({ length: 4 }, (_, index) => <article className="favorite-loading-card" key={index}><i /><span /><span /></article>)}
          </div>
        </section>
      </div>
    </div>
  );
}
