type DashboardHeroProps = {
  driver: string;
};

export const DashboardHero = ({ driver }: DashboardHeroProps) => (
  <section className="hero" id="dashboard">
    <div>
      <p className="eyebrow">Dashboard</p>
      <h2>Bot 和 Admin 已连接同一个 SQLite</h2>
      <p className="muted">V1 聚焦现有 Bot 数据管理。</p>
    </div>
    <div className="badge">{driver}</div>
  </section>
);
