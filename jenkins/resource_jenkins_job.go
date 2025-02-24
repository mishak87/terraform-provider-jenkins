package jenkins

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func resourceJenkinsJob() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceJenkinsJobCreate,
		ReadContext:   resourceJenkinsJobRead,
		UpdateContext: resourceJenkinsJobUpdate,
		DeleteContext: resourceJenkinsJobDelete,
		Schema: map[string]*schema.Schema{
			"name": {
				Type:             schema.TypeString,
				Description:      "The unique name of the JenkinsCI job.",
				Required:         true,
				ForceNew:         true,
				ValidateDiagFunc: validateJobName,
			},
			"folder": {
				Type:             schema.TypeString,
				Description:      "The folder namespace that the job will be added to.",
				Optional:         true,
				ForceNew:         true,
				ValidateDiagFunc: validateFolderName,
			},
			"template": {
				Type:             schema.TypeString,
				Description:      "The configuration file template, used to communicate with Jenkins.",
				Required:         true,
				DiffSuppressFunc: templateDiff,
			},
			"parameters": {
				Type:        schema.TypeMap,
				Description: "The set of parameters to be rendered in the template when generating a valid config.xml file.",
				Optional:    true,
				Elem:        schema.TypeString,
			},
		},
	}
}

func resourceJenkinsJobCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(jenkinsClient)
	name := d.Get("name").(string)
	folder := formatFolderName(d.Get("folder").(string))

	// Validate that the folder exists
	if err := folderExists(client, folder); err != nil {
		return diag.FromErr(fmt.Errorf("jenkins::create - Could not find folder '%s': %w", folder, err))
	}

	xml, err := renderTemplate(d.Get("template").(string), d)
	if err != nil {
		return diag.FromErr(fmt.Errorf("jenkins::create - Error binding config.xml template to %q: %w", name, err))
	}

	folders := extractFolders(folder)
	_, err = client.CreateJobInFolder(xml, name, folders...)
	if err != nil {
		return diag.FromErr(fmt.Errorf("jenkins::create - Error creating job for %q in folder %s: %w", name, folder, err))
	}

	log.Printf("[DEBUG] jenkins::create - job %q created in folder %s", name, folder)
	d.SetId(formatFolderName(folder + "/" + name))

	return resourceJenkinsJobRead(ctx, d, meta)
}

func resourceJenkinsJobRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(jenkinsClient)
	name, folders := parseCanonicalJobID(d.Id())

	log.Printf("[DEBUG] jenkins::read - Looking for job %q", name)

	job, err := client.GetJob(name, folders...)
	if err != nil {
		if strings.HasPrefix(err.Error(), "404") {
			// Job does not exist
			d.SetId("")
			return nil
		}

		return diag.FromErr(fmt.Errorf("jenkins::read - Job %q does not exist: %w", name, err))
	}

	config, err := job.GetConfig()
	if err != nil {
		return diag.FromErr(fmt.Errorf("jenkins::read - Job %q could not extract configuration: %v", job.Base, err))
	}

	log.Printf("[DEBUG] jenkins::read - Job %q exists", job.Base)
	d.SetId(job.Base)
	if err := d.Set("template", config); err != nil {
		return diag.FromErr(err)
	}

	return nil
}

func resourceJenkinsJobUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(jenkinsClient)
	name, folders := parseCanonicalJobID(d.Id())

	// grab job by current name
	job, err := client.GetJob(name, folders...)
	if err != nil {
		return diag.FromErr(fmt.Errorf("jenkins::update - Could not find job %q: %w", name, err))
	}

	xml, err := renderTemplate(d.Get("template").(string), d)
	if err != nil {
		return diag.FromErr(fmt.Errorf("jenkins::update - Error binding config.xml template to %q: %w", name, err))
	}

	err = job.UpdateConfig(xml)
	if err != nil {
		return diag.FromErr(fmt.Errorf("jenkins::update - Error updating job %q configuration: %w", name, err))
	}

	return resourceJenkinsJobRead(ctx, d, meta)
}

func resourceJenkinsJobDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(jenkinsClient)
	name, folders := parseCanonicalJobID(d.Id())

	log.Printf("[DEBUG] jenkins::delete - Removing %q", name)

	ok, err := client.DeleteJobInFolder(name, folders...)
	if err != nil {
		return diag.FromErr(err)
	}

	log.Printf("[DEBUG] jenkins::delete - %q removed: %t", name, ok)
	return nil
}
