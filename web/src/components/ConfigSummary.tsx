import type { ConfigSummary as ConfigSummaryType } from '../types';

type ConfigSummaryProps = {
  config: ConfigSummaryType | null;
};

export const ConfigSummary = ({ config }: ConfigSummaryProps) => (
  <section className="card" id="system">
    <div className="section-header">
      <div>
        <p className="eyebrow">System</p>
        <h2>配置摘要</h2>
      </div>
    </div>
    <pre>{JSON.stringify(config, null, 2)}</pre>
  </section>
);
