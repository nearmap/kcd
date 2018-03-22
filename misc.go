package main

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/nearmap/cvmanager/ecr"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type ecrTagParams struct {
	tags    []string
	ecr     string
	version string

	stats statsParams
}

// newECRTagCommand is CLI interface to managing tags on ECR images
func newECRTagCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ecr-tags",
		Short: "Manages tags of ECR repository",
		Long:  "Manages adds/removes tags on ECR repositories",
	}

	var params ecrTagParams

	cmd.PersistentFlags().StringVar(&params.ecr, "ecr", "", "ECR repository ARN ex. nearmap/cvmanager")
	cmd.PersistentFlags().StringSliceVar(&params.tags, "tags", nil, "list of tags that needs to be added or removed")
	cmd.PersistentFlags().StringVar(&params.version, "version", "", "sha/version tag of ECR image that is being tagged")
	(&params.stats).addFlags(cmd)

	cmd.PreRunE = func(cmd *cobra.Command, args []string) (err error) {
		if params.ecr == "" {
			return errors.New("ecr repository is required.")
		}
		return nil
	}

	addTagCmd := &cobra.Command{
		Use:   "add",
		Short: "Add tag to image in given ecr repository",
		Long:  "Add tag to image in given ecr repository",
	}
	addTagCmd.PreRunE = func(cmd *cobra.Command, args []string) (err error) {
		if params.tags == nil || len(params.tags) == 0 || params.version == "" {
			return errors.New("ecr image version is required.")
		}
		return nil
	}
	addTagCmd.RunE = func(cmd *cobra.Command, args []string) error {
		sess, err := session.NewSession()
		if err != nil {
			return errors.Wrap(err, "failed to obtain AWS session")
		}

		stats, err := params.stats.stats("ecr")
		if err != nil {
			return errors.Wrap(err, "failed to initialize stats")
		}
		tagger := ecr.NewTagger(sess, stats)
		return tagger.Add(params.ecr, params.version, params.tags...)
	}

	rmTagCmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove tag to image in given ecr repository",
		Long:  "Remove tag to image in given ecr repository",
	}
	rmTagCmd.PreRunE = func(cmd *cobra.Command, args []string) (err error) {
		if params.tags == nil || len(params.tags) == 0 {
			return errors.New("tags are required.")
		}
		return nil
	}
	rmTagCmd.RunE = func(cmd *cobra.Command, args []string) error {
		sess, err := session.NewSession()
		if err != nil {
			return errors.Wrap(err, "failed to obtain AWS session")
		}

		stats, err := params.stats.stats("ecr")
		if err != nil {
			return errors.Wrap(err, "failed to initialize stats")
		}
		tagger := ecr.NewTagger(sess, stats)
		return tagger.Remove(params.ecr, params.tags...)
	}

	getTagCmd := &cobra.Command{
		Use:   "get",
		Short: "get tags of image by its version tagin given ecr repository",
		Long:  "Remove tag to image in given ecr repository",
	}
	getTagCmd.PreRunE = func(cmd *cobra.Command, args []string) (err error) {
		if params.version == "" {
			return errors.New("version is required.")
		}
		return nil
	}
	getTagCmd.RunE = func(cmd *cobra.Command, args []string) error {
		sess, err := session.NewSession()
		if err != nil {
			return errors.Wrap(err, "failed to obtain AWS session")
		}

		stats, err := params.stats.stats("ecr")
		if err != nil {
			return errors.Wrap(err, "failed to initialize stats")
		}
		tagger := ecr.NewTagger(sess, stats)
		t, err := tagger.Get(params.ecr, params.version)
		fmt.Printf("Found tags %s on requested ECR repository of image %s \n", t, params.version)
		return err
	}

	cmd.AddCommand(addTagCmd)
	cmd.AddCommand(rmTagCmd)
	cmd.AddCommand(getTagCmd)

	return cmd
}
