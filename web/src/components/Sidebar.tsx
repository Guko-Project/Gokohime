import type { AdminUser } from '../types';

type SidebarProps = {
  user: AdminUser;
  onLogout: () => void;
};

export const Sidebar = ({ user, onLogout }: SidebarProps) => (
  <aside className="sidebar">
    <div>
      <p className="eyebrow">Gokohime</p>
      <h1>Admin</h1>
    </div>
    <nav>
      <a href="#dashboard">Dashboard</a>
      <a href="#bot-data">Bot 数据</a>
      <a href="#system">系统配置</a>
    </nav>
    <button className="button ghost" onClick={onLogout} type="button">
      退出 {user.username}
    </button>
  </aside>
);
