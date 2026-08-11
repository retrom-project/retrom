import { PageHeader } from "@/components/ui";

export default function FavoritesLoading() {
  return (
    <div className="page-layout favorite-page favorite-loading-shell" role="status" aria-label="正在加载收藏" aria-live="polite">
      <span className="sr-only">正在加载收藏</span>
      <PageHeader eyebrow="你的游戏" title="我的收藏" description="正在读取你的收藏与收藏夹。" />
      <div className="favorite-head-summary favorite-skeleton-line" aria-hidden="true" />
      <div className="favorite-layout" aria-hidden="true">
        <aside className="favorite-rail">
          <header><h2>收藏导航</h2><p>全部收藏与自定义收藏夹</p></header>
          <div className="favorite-skeleton-rail"><i /><i /><i /><i /></div>
          <div className="favorite-skeleton-control" />
        </aside>
        <section className="favorite-content">
          <header className="favorite-view-head"><div><h2>全部收藏</h2><p>正在加载可见游戏</p></div></header>
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
