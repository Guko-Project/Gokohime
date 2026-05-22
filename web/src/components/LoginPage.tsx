import { useState } from 'react';

type LoginPageProps = {
  onLogin: (username: string, password: string) => Promise<void>;
  error: string;
  loading: boolean;
};

export const LoginPage = ({ onLogin, error, loading }: LoginPageProps) => {
  const [username, setUsername] = useState('admin');
  const [password, setPassword] = useState('');

  const handleSubmit = (event: React.FormEvent) => {
    event.preventDefault();
    void onLogin(username, password).then(() => setPassword(''));
  };

  return (
    <main className="login-page">
      <form className="card login-card" onSubmit={handleSubmit}>
        <div>
          <p className="eyebrow">Gokohime Admin</p>
          <h1>登录管理端</h1>
          <p className="muted">使用配置中的初始管理员账号进入后台。</p>
        </div>
        <label>
          用户名
          <input
            value={username}
            onChange={(event) => setUsername(event.target.value)}
            autoComplete="username"
          />
        </label>
        <label>
          密码
          <input
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            type="password"
            autoComplete="current-password"
          />
        </label>
        {error ? <p className="error">{error}</p> : null}
        <button className="button" disabled={loading} type="submit">
          {loading ? '登录中...' : '登录'}
        </button>
      </form>
    </main>
  );
};
