// ─── Connect Repo Page ───────────────────────────────────────────────────────
// Simple form to connect a new repository by providing its URL.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { ArrowLeft, FolderGit2 } from "lucide-react";
import Link from "next/link";
import { useCreateRepo } from "@/lib/api/mutations";
import { PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useUIStore } from "@/lib/stores/ui";

const schema = z.object({
  clone_url: z.string().url("Must be a valid URL"),
  name: z.string().min(1, "Name is required"),
  platform: z.enum(["github", "gitlab", "bitbucket", "azure-devops"]),
  default_branch: z.string().min(1).default("main"),
});

type FormData = z.infer<typeof schema>;

export default function ConnectRepoPage() {
  const router = useRouter();
  const createRepo = useCreateRepo();
  const { addToast } = useUIStore();

  const {
    register,
    handleSubmit,
    setValue,
    formState: { errors, isSubmitting },
  } = useForm<FormData>({
    resolver: zodResolver(schema),
    defaultValues: {
      platform: "github",
      default_branch: "main",
    },
  });

  const onSubmit = async (data: FormData) => {
    try {
      await createRepo.mutateAsync({
        clone_url: data.clone_url,
        name: data.name,
        platform: data.platform,
        default_branch: data.default_branch,
      });
      addToast({
        title: "Repository connected",
        description: `${data.name} has been added successfully.`,
        variant: "success",
      });
      router.push("/repos");
    } catch (err) {
      addToast({
        title: "Failed to connect repository",
        description: err instanceof Error ? err.message : "Unknown error",
        variant: "error",
      });
    }
  };

  return (
    <>
      <PageHeader
        title="Connect Repository"
        description="Add a new repository for AI code review"
        actions={
          <Button variant="outline" size="sm" asChild>
            <Link href="/repos">
              <ArrowLeft className="mr-1 h-4 w-4" />
              Back
            </Link>
          </Button>
        }
      />

      <Card className="max-w-2xl">
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <FolderGit2 className="h-5 w-5" />
            Repository Details
          </CardTitle>
          <CardDescription>
            Provide the repository information to start AI-powered code reviews.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="name">Repository Name</Label>
              <Input
                id="name"
                placeholder="my-project"
                {...register("name")}
              />
              {errors.name && (
                <p className="text-xs text-red-500">{errors.name.message}</p>
              )}
            </div>

            <div className="space-y-2">
              <Label htmlFor="clone_url">Clone URL</Label>
              <Input
                id="clone_url"
                placeholder="https://github.com/org/repo.git"
                {...register("clone_url")}
              />
              {errors.clone_url && (
                <p className="text-xs text-red-500">
                  {errors.clone_url.message}
                </p>
              )}
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label>Platform</Label>
                <Select
                  defaultValue="github"
                  onValueChange={(val) =>
                    setValue(
                      "platform",
                      val as FormData["platform"],
                    )
                  }
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="github">GitHub</SelectItem>
                    <SelectItem value="gitlab">GitLab</SelectItem>
                    <SelectItem value="bitbucket">Bitbucket</SelectItem>
                    <SelectItem value="azure-devops">Azure DevOps</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <div className="space-y-2">
                <Label htmlFor="default_branch">Default Branch</Label>
                <Input
                  id="default_branch"
                  placeholder="main"
                  {...register("default_branch")}
                />
              </div>
            </div>

            <div className="flex justify-end pt-4">
              <Button type="submit" disabled={isSubmitting}>
                {isSubmitting ? "Connecting…" : "Connect Repository"}
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>
    </>
  );
}
