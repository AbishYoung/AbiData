import { useNavigate } from "@tanstack/react-router";
import { queryClient } from "@/app/queryClient";
import { adoptSession, login } from "@/lib/session";
import { AuthLayout } from "./AuthLayout";
import { LoginForm } from "./LoginForm";

export function LoginPage() {
  const navigate = useNavigate();
  return (
    <AuthLayout
      title="Sign in"
      subtitle="Welcome back. Enter your details to open your workspaces."
    >
      <LoginForm
        onSubmit={async (email, password) => {
          const me = await login(email, password);
          adoptSession(queryClient, me);
          await navigate({ to: "/" });
        }}
      />
    </AuthLayout>
  );
}
